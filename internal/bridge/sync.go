package bridge

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const dataDir = "music-bridge"
const libraryDir = "Library"
const marker = ".music-bridge-target"
const manifest = ".music-bridge-manifest.json"
const pendingManifest = ".music-bridge-pending-manifest"
const completionSound = "/System/Library/Sounds/Glass.aiff"

type Planned struct {
	Track    Track
	Relative string
	Size     int64
}

type syncMode string

const (
	driveSyncMode   syncMode = "drive"
	androidSyncMode syncMode = "android"
)

func Run(argv []string) error {
	closeLog, logErr := startDiagnosticLog()
	if logErr == nil {
		defer closeLog()
	}
	err := runSync(argv)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		logf("error: %v", err)
	}
	return err
}

func DiagnosticLogPath() string { return diagnosticLogPath() }

func NotifyCompletion() {
	// Terminal.appはBELでDockバッジ・Dockアイコンのバウンスを表示できる。
	fmt.Fprint(os.Stderr, "\a")
	_ = exec.Command("afplay", completionSound).Run()
}

func runSync(argv []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	initTarget := fs.Bool("init-target", false, "initialize target")
	dryRun := fs.Bool("dry-run", false, "dry run")
	refresh := fs.Bool("refresh", false, "refresh playlist cache from Music.app")
	source := fs.String("source-json", "", "JSON source")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("サブコマンドは廃止されました。music-bridge [options] で実行してください")
	}
	mode, err := chooseSyncMode()
	if err != nil {
		return err
	}
	if mode == androidSyncMode {
		return runAndroidSync(androidSyncOptions{
			InitTarget: *initTarget,
			DryRun:     *dryRun, Refresh: *refresh, Source: *source,
		})
	}
	volume, err := chooseTarget()
	if err != nil {
		return err
	}
	if err := setDiagnosticLogContext("drive-" + filepath.Base(volume)); err != nil {
		logf("diagnostic log rename failed: %v", err)
	}
	root := filepath.Join(volume, dataDir)
	markerPath := filepath.Join(root, marker)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		if !*initTarget {
			if !confirmDefaultYes(fmt.Sprintf("%sを初期化しますか？ [Y/n] ", root)) {
				return fmt.Errorf("同期先の初期化をキャンセルしました")
			}
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(markerPath, []byte("Music Bridge target\n"), 0644); err != nil {
			return err
		}
	}
	unlock, err := lockTarget(root)
	if err != nil {
		return err
	}
	defer unlock()
	if !*dryRun {
		if err := migrateLegacyLayout(root); err != nil {
			return err
		}
	}
	summaries, err := loadPlaylists(*source, true, nil)
	if err != nil {
		return err
	}
	selected, err := chooseMany(summaries, root)
	if err != nil {
		return err
	}
	artworkDir, err := os.MkdirTemp("", "music-bridge-artwork-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(artworkDir)
	playlists, err := loadSyncPlaylists(*source, selected, *refresh)
	if err != nil {
		return err
	}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		return err
	}
	if err := validatePlan(plan, playlists); err != nil {
		return err
	}
	artworkDirs := map[string]bool{}
	if !*dryRun {
		// 容量不足の場合、今回の同期対象にならない曲のジャケ写を取得しても
		// 配置されず、次回も同じ問い合わせを繰り返す。まず音源だけで収まる
		// 範囲を仮決定し、その範囲のアルバムだけをMusic.appへ問い合わせる。
		artworkPlan := plan
		audioWithoutArtwork := makeAudioTransferPlan(plan, root).bytes
		freeBeforeArtwork, err := freeBytes(volume)
		if err != nil {
			return err
		}
		if audioWithoutArtwork > freeBeforeArtwork {
			artworkPlan = fitPlan(plan, root, freeBeforeArtwork)
		}
		artworkDirs = artworkCandidateDirs(artworkPlan, root)
		if err := exportArtworks(playlists, artworkDir, artworkRequests(playlists, artworkPlan)); err != nil {
			return err
		}
		// exportArtworks は playlists 内の Track.Artwork を更新する。転送計画は
		// Track の値を保持しているため、画像の配置・容量計算へ反映させるには
		// ここで作り直す必要がある。
		plan, missing, err = makePlan(playlists)
		if err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		fmt.Printf("ローカルファイルなし: %d曲\n", len(missing))
	}
	cleanupPlan := append([]Planned(nil), plan...)
	audioTransfers := makeAudioTransferPlan(plan, root)
	artworkTransfers, err := makeArtworkTransferPlan(plan, root, artworkDirs)
	if err != nil {
		return err
	}
	audioRequired := audioTransfers.bytes
	artworkRequired := artworkTransfers.bytes
	required := audioRequired + artworkRequired
	free, err := freeBytes(volume)
	if err != nil {
		return err
	}
	fmt.Printf("選択プレイリスト: %d件 / 曲: %d曲\n", len(playlists), countTracks(playlists))
	fmt.Printf("新規転送容量: 音源 %s + ジャケ写 %s = %s / 空き容量: %s\n", humanBytes(audioRequired), humanBytes(artworkRequired), humanBytes(required), humanBytes(free))
	if required > free {
		fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Println("!!! 警告: 容量が不足しています。                  !!!")
		fmt.Printf("!!! 必要容量: %s / 空き容量: %s / 不足: %s !!!\n", humanBytes(required), humanBytes(free), humanBytes(required-free))
		fmt.Println("!!! 空き容量に収まる範囲で同期を続行します。      !!!")
		artworkBudget := free - artworkRequired
		if artworkBudget < 0 {
			artworkBudget = 0
		}
		plan = fitPlan(plan, root, artworkBudget)
		audioTransfers = makeAudioTransferPlan(plan, root)
		artworkTransfers, err = makeArtworkTransferPlan(plan, root, artworkDirs)
		if err != nil {
			return err
		}
	}
	playlistTransfers, err := makePlaylistSyncPlan(playlists, plan, root)
	if err != nil {
		return err
	}
	if len(playlistTransfers.stale) > 0 {
		fmt.Printf("警告: 選択されなかったプレイリストのM3Uを削除します（%d件）\n", len(playlistTransfers.stale))
	}
	toDelete, deleteBytes := staleAudio(cleanupPlan, root)
	if len(toDelete) > 0 {
		fmt.Printf("警告: 選択されなかった音源を削除します（%dファイル / %s）\n", len(toDelete), humanBytes(deleteBytes))
	}
	artworkToDelete, artworkDeleteBytes, err := staleArtwork(cleanupPlan, root)
	if err != nil {
		return err
	}
	if len(artworkToDelete) > 0 {
		fmt.Printf("警告: 選択されなかったアルバムのジャケ写を削除します（%dファイル / %s）\n",
			len(artworkToDelete), humanBytes(artworkDeleteBytes))
	}
	managementToDelete := staleManagementFiles(root)
	if !*dryRun {
		if err := savePendingManifest(root, plan); err != nil {
			return err
		}
	}
	labels := map[string]string{}
	for _, p := range playlists {
		for _, t := range p.Tracks {
			if t.Location != "" && labels[t.Location] == "" {
				labels[t.Location] = p.Name
			}
		}
	}
	if err := playlistTransfers.write(*dryRun); err != nil {
		return err
	}
	if err := artworkTransfers.write(*dryRun); err != nil {
		return err
	}
	if err := transfer(audioTransfers, root, *dryRun, labels); err != nil {
		return err
	}
	if !*dryRun {
		if err := playlistTransfers.removeStale(false); err != nil {
			return err
		}
		for _, path := range toDelete {
			if err := os.Remove(filepath.Join(root, path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		for _, path := range artworkToDelete {
			if err := os.Remove(filepath.Join(root, path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		for _, path := range managementToDelete {
			if err := os.Remove(filepath.Join(root, path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := removeEmptyDirs(root); err != nil {
			return err
		}
		if err := saveManifest(root, plan); err != nil {
			return err
		}
	}
	fmt.Printf("転送完了: %d/%d曲\n同期完了: %dプレイリスト\n", len(plan), len(plan), len(playlists))
	return nil
}
