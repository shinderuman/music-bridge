package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const dataDir = "music-bridge"
const libraryDir = "Library"
const marker = ".music-bridge-target"
const manifest = ".music-bridge-manifest.json"
const completionSound = "/System/Library/Sounds/Glass.aiff"

type Planned struct {
	Track    Track
	Relative string
	Size     int64
}

func main() {
	if len(os.Args) < 2 {
		usage()
		notifyCompletion()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "playlists":
		if err := runPlaylists(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "sync":
		if err := runSync(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
		notifyCompletion()
		os.Exit(2)
	}
	notifyCompletion()
}

func usage() {
	fmt.Println("usage: music-bridge {playlists|sync} [--target PATH] [--init-target] [--dry-run] [--refresh]")
}
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "music-bridge:", err)
	notifyCompletion()
	os.Exit(1)
}

func notifyCompletion() {
	// Terminal.appはBELでDockバッジ・Dockアイコンのバウンスを表示できる。
	fmt.Fprint(os.Stderr, "\a")
	_ = exec.Command("afplay", completionSound).Run()
}

func runSync(argv []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	target := fs.String("target", "", "target volume")
	initTarget := fs.Bool("init-target", false, "initialize target")
	dryRun := fs.Bool("dry-run", false, "dry run")
	refresh := fs.Bool("refresh", false, "refresh playlist cache from Music.app")
	source := fs.String("source-json", "", "JSON source")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	volume, err := chooseTarget(*target)
	if err != nil {
		return err
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
		audioWithoutArtwork, err := existingBytes(plan, root)
		if err != nil {
			return err
		}
		freeBeforeArtwork, err := freeBytes(volume)
		if err != nil {
			return err
		}
		if audioWithoutArtwork > freeBeforeArtwork {
			artworkPlan = fitPlan(plan, root, freeBeforeArtwork)
		}
		artworkDirs = artworkCandidateDirs(artworkPlan, root)
		if err := exportArtworks(playlists, artworkDir, artworkRequests(playlists, artworkPlan, artworkDirs)); err != nil {
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
	audioRequired, err := existingBytes(plan, root)
	if err != nil {
		return err
	}
	artworkRequired, err := artworkBytes(plan, root)
	if err != nil {
		return err
	}
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
	}
	stale := stalePlaylists(summaries, selected, root)
	if len(stale) > 0 {
		fmt.Printf("警告: 選択されなかったプレイリストのM3Uを削除します（%d件）\n", len(stale))
	}
	toDelete, deleteBytes := staleAudio(cleanupPlan, root)
	if len(toDelete) > 0 {
		fmt.Printf("警告: 選択されなかった音源を削除します（%dファイル / %s）\n", len(toDelete), humanBytes(deleteBytes))
	}
	labels := map[string]string{}
	for _, p := range playlists {
		for _, t := range p.Tracks {
			if t.Location != "" && labels[t.Location] == "" {
				labels[t.Location] = p.Name
			}
		}
	}
	if err := writePlaylists(playlists, plan, root, *dryRun); err != nil {
		return err
	}
	if err := writeArtworks(plan, root, *dryRun, artworkDirs); err != nil {
		return err
	}
	if err := transfer(plan, root, *dryRun, labels); err != nil {
		return err
	}
	if !*dryRun {
		for _, path := range stale {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		for _, path := range toDelete {
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

func countTracks(playlists []Playlist) int {
	total := 0
	for _, p := range playlists {
		total += len(p.Tracks)
	}
	return total
}

func validatePlan(plan []Planned, playlists []Playlist) error {
	if len(plan) == 0 && countTracks(playlists) > 0 {
		return fmt.Errorf("同期可能なローカル音源がありません。Musicライブラリの保存先ボリュームが接続されているか確認してください")
	}
	return nil
}

func confirmDefaultYes(prompt string) bool {
	fmt.Print(prompt)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func makePlan(playlists []Playlist) ([]Planned, []string, error) {
	seen := map[string]bool{}
	var plan []Planned
	var missing []string
	for _, p := range playlists {
		for _, t := range p.Tracks {
			if t.Location == "" {
				missing = append(missing, t.Name)
				continue
			}
			info, err := os.Stat(t.Location)
			if err != nil || !info.Mode().IsRegular() {
				missing = append(missing, t.Name)
				continue
			}
			if seen[t.Location] {
				continue
			}
			seen[t.Location] = true
			artist := t.AlbumArtist
			if artist == "" {
				artist = t.Artist
			}
			rel := filepath.Join(libraryDir, artist, t.Album, filepath.Base(t.Location))
			plan = append(plan, Planned{t, rel, info.Size()})
		}
	}
	return plan, missing, nil
}

func totalBytes(plan []Planned) int64 {
	var total int64
	for _, p := range plan {
		total += p.Size
	}
	return total
}
func humanBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func freeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func sameFile(source, destination string) bool {
	ai, err := os.Stat(source)
	if err != nil {
		return false
	}
	bi, err := os.Stat(destination)
	if err != nil {
		return false
	}
	modTimeDelta := ai.ModTime().Sub(bi.ModTime())
	if modTimeDelta < 0 {
		modTimeDelta = -modTimeDelta
	}
	// exFATでは更新日時の精度が低く、macOS側と数ms〜数十msずれる。
	return ai.Mode().IsRegular() && bi.Mode().IsRegular() &&
		ai.Size() == bi.Size() && modTimeDelta <= 2*time.Second
}

func existingBytes(plan []Planned, root string) (int64, error) {
	var total int64
	for _, p := range plan {
		destination := filepath.Join(root, p.Relative)
		if !sameFile(p.Track.Location, destination) {
			total += p.Size
		}
	}
	return total, nil
}

func loadManifest(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		return nil
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return nil
	}
	return paths
}

func staleAudio(plan []Planned, root string) ([]string, int64) {
	desired := map[string]bool{}
	for _, item := range plan {
		desired[item.Relative] = true
	}
	var stale []string
	var total int64
	for _, relative := range loadManifest(root) {
		if desired[relative] {
			continue
		}
		info, err := os.Stat(filepath.Join(root, relative))
		if err == nil && info.Mode().IsRegular() {
			stale = append(stale, relative)
			total += info.Size()
		}
	}
	sort.Strings(stale)
	return stale, total
}

func saveManifest(root string, plan []Planned) error {
	paths := make([]string, 0, len(plan))
	for _, item := range plan {
		paths = append(paths, item.Relative)
	}
	sort.Strings(paths)
	return saveManifestPaths(root, paths)
}

func saveManifestPaths(root string, paths []string) error {
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, manifest), append(data, '\n'), 0644)
}

func migrateLegacyLayout(root string) error {
	libraryRoot := filepath.Join(root, libraryDir)
	if info, err := os.Stat(libraryRoot); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("ライブラリディレクトリがディレクトリではありません: %s", libraryRoot)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var legacyDirs []string
	type playlistFile struct {
		path string
		data []byte
	}
	var playlists []playlistFile
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			legacyDirs = append(legacyDirs, entry.Name())
			continue
		}
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".m3u" {
			path := filepath.Join(root, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			playlists = append(playlists, playlistFile{path, prefixLibraryInM3U(data)})
		}
	}
	if len(legacyDirs) == 0 {
		return nil
	}

	manifestPaths := loadManifest(root)
	for i, path := range manifestPaths {
		if !strings.HasPrefix(filepath.ToSlash(path), libraryDir+"/") {
			manifestPaths[i] = filepath.Join(libraryDir, path)
		}
	}

	fmt.Printf("既存ライブラリを %s/ へ移行中...\n", libraryDir)
	if err := os.Mkdir(libraryRoot, 0755); err != nil {
		return err
	}
	for _, name := range legacyDirs {
		if err := os.Rename(filepath.Join(root, name), filepath.Join(libraryRoot, name)); err != nil {
			return err
		}
	}
	for _, playlist := range playlists {
		if err := os.WriteFile(playlist.path, playlist.data, 0644); err != nil {
			return err
		}
	}
	if len(manifestPaths) > 0 {
		sort.Strings(manifestPaths)
		if err := saveManifestPaths(root, manifestPaths); err != nil {
			return err
		}
	}
	fmt.Printf("ライブラリ移行完了: %dディレクトリ\n", len(legacyDirs))
	return nil
}

func prefixLibraryInM3U(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		ending := ""
		if strings.HasSuffix(line, "\r") {
			line = strings.TrimSuffix(line, "\r")
			ending = "\r"
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "\ufeff#") || strings.HasPrefix(line, libraryDir+"/") {
			continue
		}
		lines[i] = filepath.ToSlash(filepath.Join(libraryDir, line)) + ending
	}
	return []byte(strings.Join(lines, "\n"))
}

func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
	}
	return nil
}

func fitPlan(plan []Planned, root string, free int64) []Planned {
	result := []Planned{}
	for _, p := range plan {
		destination := filepath.Join(root, p.Relative)
		if sameFile(p.Track.Location, destination) {
			result = append(result, p)
			continue
		}
		if p.Size <= free {
			result = append(result, p)
			free -= p.Size
		}
	}
	return result
}

func transfer(plan []Planned, root string, dry bool, labels map[string]string) error {
	started := time.Now()
	pending := make([]Planned, 0, len(plan))
	for _, item := range plan {
		if !sameFile(item.Track.Location, filepath.Join(root, item.Relative)) {
			pending = append(pending, item)
		}
	}
	total := totalBytes(pending)
	var done int64
	const maxBatchBytes int64 = 1 << 30
	batches := make([][]Planned, 0)
	for _, item := range pending {
		if len(batches) == 0 || (batchBytes(batches[len(batches)-1])+item.Size > maxBatchBytes && len(batches[len(batches)-1]) > 0) {
			batches = append(batches, nil)
		}
		batches[len(batches)-1] = append(batches[len(batches)-1], item)
	}
	processed := 0
	// ETA と速度は、完了した rsync バッチの実測時間だけから算出する。
	// スピナーの再描画時刻を分母に含めると、バッチ転送中に見かけの速度が
	// 毎秒下がり、ETA が毎秒増えてしまう。
	var transferElapsed time.Duration
	var displayMu sync.Mutex
	printProgress := func(item Planned, spinner string) {
		displayMu.Lock()
		defer displayMu.Unlock()
		rate, eta := transferEstimate(total, done, transferElapsed)
		etaText := "-"
		speed := "-"
		if rate > 0 {
			etaText = eta.Round(time.Second).String()
			speed = humanBytes(rate) + "/s"
		}
		percent := 100.0
		if total > 0 {
			percent = float64(done) * 100 / float64(total)
		}
		label := labels[item.Track.Location]
		if label != "" {
			label = " | プレイリスト: " + label
		}
		activity := ""
		if spinner != "" {
			activity = " | コピー中 " + spinner
		}
		fmt.Printf("\033[2K\rETA %s | 速度 %s | 転送中 [%d/%d曲] %5.1f%% (%s/%s)%s | %s%s",
			etaText, speed, processed, len(pending), percent,
			humanBytes(done), humanBytes(total), label, truncateRunes(item.Track.Name, 20), activity)
	}
	for batchIndex, items := range batches {
		stage, err := stagePlan(items)
		if err != nil {
			return err
		}
		func() {
			defer os.RemoveAll(stage)
			args := []string{"-ahL", "--partial", "--append-verify"}
			if dry {
				args = append(args, "--dry-run")
			}
			// 転送は常に1本で実行し、microSD上の帯域競合を避ける。
			if len(items) > 0 {
				printProgress(items[0], "|")
			}
			args = append(args, stage+string(os.PathSeparator), root+string(os.PathSeparator))
			cmd := exec.Command("rsync", args...)
			batchStarted := time.Now()
			stopSpinner := make(chan struct{})
			var spinnerWG sync.WaitGroup
			if len(items) > 0 && !dry {
				spinnerWG.Add(1)
				go func(item Planned) {
					defer spinnerWG.Done()
					frames := []string{"|", "/", "-", "\\"}
					frame := 0
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							frame = (frame + 1) % len(frames)
							printProgress(item, frames[frame])
						case <-stopSpinner:
							return
						}
					}
				}(items[0])
			}
			runErr := cmd.Run()
			close(stopSpinner)
			spinnerWG.Wait()
			if runErr != nil {
				err = runErr
				return
			}
			transferElapsed += time.Since(batchStarted)
			for _, item := range items {
				processed++
				done += item.Size
				printProgress(item, "")
			}
		}()
		if err != nil {
			fmt.Print("\033[2K\r")
			return fmt.Errorf("転送バッチ %d/%d: %w", batchIndex+1, len(batches), err)
		}
	}
	fmt.Print("\033[2K\r")
	fmt.Println()
	fmt.Printf("音源転送時間: %s\n", time.Since(started).Round(time.Second))
	return nil
}

func transferEstimate(total, done int64, elapsed time.Duration) (int64, time.Duration) {
	if done <= 0 || elapsed <= 0 || total <= done {
		return 0, 0
	}
	rate := int64(float64(done) / elapsed.Seconds())
	if rate <= 0 {
		return 0, 0
	}
	return rate, time.Duration(float64(total-done)/float64(rate)) * time.Second
}

func batchBytes(items []Planned) int64 {
	var total int64
	for _, item := range items {
		total += item.Size
	}
	return total
}

func artworkBytes(plan []Planned, root string) (int64, error) {
	seen := map[string]bool{}
	var total int64
	for _, item := range plan {
		if item.Track.Artwork == "" {
			continue
		}
		destinationDir := filepath.Join(root, filepath.Dir(item.Relative))
		if seen[destinationDir] {
			continue
		}
		sourceInfo, err := os.Stat(item.Track.Artwork)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if !sourceInfo.Mode().IsRegular() || sameFile(item.Track.Artwork, filepath.Join(destinationDir, "AlbumArt.jpg")) {
			if sourceInfo.Mode().IsRegular() {
				seen[destinationDir] = true
			}
			continue
		}
		seen[destinationDir] = true
		total += sourceInfo.Size()
	}
	return total, nil
}

func writeArtworks(plan []Planned, root string, dry bool, artworkDirs map[string]bool) error {
	started := time.Now()
	type artworkCopy struct {
		source      string
		destination string
		modTime     time.Time
	}
	plannedDirs := map[string]string{}
	sources := map[string]string{}
	for _, item := range plan {
		relativeDir := filepath.Dir(item.Relative)
		destinationDir := filepath.Join(root, relativeDir)
		plannedDirs[destinationDir] = relativeDir
		if item.Track.Artwork == "" || sources[destinationDir] != "" {
			continue
		}
		info, err := os.Stat(item.Track.Artwork)
		if err == nil && info.Mode().IsRegular() {
			sources[destinationDir] = item.Track.Artwork
		}
	}
	available := 0
	var copies []artworkCopy
	var emptyArtworkDirs []string
	for destinationDir, relativeDir := range plannedDirs {
		if !artworkDirs[relativeDir] {
			continue
		}
		destination := filepath.Join(destinationDir, "AlbumArt.jpg")
		source := sources[destinationDir]
		if source == "" {
			emptyArtworkDirs = append(emptyArtworkDirs, destinationDir)
			continue
		}
		sourceInfo, err := os.Stat(source)
		if err != nil || !sourceInfo.Mode().IsRegular() {
			continue
		}
		available++
		if sameFile(source, destination) {
			continue
		}
		copies = append(copies, artworkCopy{source, destination, sourceInfo.ModTime()})
	}
	sort.Slice(copies, func(i, j int) bool { return copies[i].destination < copies[j].destination })
	for _, destinationDir := range emptyArtworkDirs {
		if dry {
			continue
		}
		if err := os.MkdirAll(destinationDir, 0755); err != nil {
			return err
		}
	}
	if len(copies) > 0 && !dry {
		lastDraw := time.Time{}
		drawProgress := func(done int) {
			percent := float64(done) * 100 / float64(len(copies))
			fmt.Printf("\033[2K\rジャケ写を配置中 [%d/%d] %5.1f%%", done, len(copies), percent)
			lastDraw = time.Now()
		}
		drawProgress(0)
		for index, copy := range copies {
			if err := os.MkdirAll(filepath.Dir(copy.destination), 0755); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			if err := copyFile(copy.source, copy.destination, copy.modTime); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			done := index + 1
			if done == len(copies) || done%10 == 0 || time.Since(lastDraw) >= time.Second {
				drawProgress(done)
			}
		}
		fmt.Print("\033[2K\r\n")
	}
	fmt.Printf("ジャケ写: %d件取得 / %d件配置（処理時間: %s）\n", available, len(copies), time.Since(started).Round(time.Millisecond))
	return nil
}

func copyFile(source, destination string, modTime time.Time) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(destination, modTime, modTime)
}

func stagePlan(items []Planned) (string, error) {
	stage, err := os.MkdirTemp("", "music-bridge-stage-")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		path := filepath.Join(stage, item.Relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			os.RemoveAll(stage)
			return "", err
		}
		if err := os.Symlink(item.Track.Location, path); err != nil {
			os.RemoveAll(stage)
			return "", err
		}
	}
	return stage, nil
}

func writePlaylists(playlists []Playlist, plan []Planned, root string, dry bool) error {
	available := map[string]string{}
	for _, item := range plan {
		available[item.Track.Location] = filepath.ToSlash(item.Relative)
	}
	for i, p := range playlists {
		fmt.Printf("プレイリスト生成中 [%d/%d] %s\n", i+1, len(playlists), p.Name)
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		for _, t := range p.Tracks {
			relative, ok := available[t.Location]
			if t.Location == "" || !ok {
				continue
			}
			b.WriteString(relative)
			b.WriteByte('\n')
		}
		if !dry {
			path := filepath.Join(root, safeName(p.Name)+".m3u")
			if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(b.String())...), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func stalePlaylists(all, selected []Playlist, root string) []string {
	wanted := map[string]bool{}
	for _, p := range selected {
		wanted[safeName(p.Name)] = true
	}
	known := map[string]bool{}
	for _, p := range all {
		known[safeName(p.Name)] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".m3u" {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".m3u")
		if known[stem] && !wanted[stem] {
			result = append(result, filepath.Join(root, e.Name()))
		}
	}
	sort.Strings(result)
	return result
}
func safeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")
	if s == "" {
		return "Unknown"
	}
	return s
}
