package android

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Options struct {
	Device     string
	Storage    string
	InitTarget bool
	DryRun     bool
	Refresh    bool
	Source     string
}

type androidContent struct {
	Source   string
	Relative string
	Name     string
	Kind     string
	Size     int64
}

type androidTransferPlan struct {
	content       []androidContent
	pending       []androidContent
	audioBytes    int64
	artworkBytes  int64
	playlistBytes int64
}

var androidCacheDir = os.UserCacheDir
var androidPlaylistSelector = chooseManyWithExisting

func Run(options Options) error {
	ctx := context.Background()
	device, err := chooseAndroidDevice(options.Device)
	if err != nil {
		return err
	}
	fmt.Println(androidDeviceSelectionMessage(device))
	volume, err := chooseAndroidVolume(device, options.Storage)
	if err != nil {
		return err
	}
	if err := setDiagnosticLogContext(
		"android-" + androidDeviceName(device) + "-" + androidVolumeName(volume),
	); err != nil {
		logf("diagnostic log rename failed: %v", err)
	}
	fmt.Printf("同期先: %s\n", androidVolumeLabel(volume))

	connection := newAndroidConnection(device.Serial, androidDeviceName(device))
	wait := connection.Wait
	deviceIdentity, err := retryAndroidValue(ctx, func() (string, error) {
		return androidDeviceLockIdentity(connection.Serial())
	}, wait)
	if err != nil {
		return err
	}
	connection.SetIdentity(deviceIdentity)
	root := path.Join(volume.Path, dataDir)
	unlock, err := lockAndroidTarget(deviceIdentity, androidVolumeLockIdentity(volume))
	if err != nil {
		return err
	}
	defer unlock()

	if err := initializeAndroidTarget(ctx, connection.Serial, root, options.InitTarget, options.DryRun, wait); err != nil {
		return err
	}
	inventory, err := loadAndroidInventoryWithProgress(ctx, connection.Serial, root, wait)
	if err != nil {
		return err
	}
	existingPlaylists := map[string]bool{}
	existingPlaylistsFolded := map[string]bool{}
	for relative := range inventory {
		if path.Ext(relative) == ".m3u" && !strings.Contains(relative, "/") && !isAppleDoublePath(relative) {
			name := logicalPathFromAndroid(strings.TrimSuffix(relative, ".m3u"))
			existingPlaylists[name] = true
			existingPlaylistsFolded[portablePathKey(name)] = true
		}
	}

	var summaries, selected, playlists []Playlist
	var plan []Planned
	var missing []string
	err = withAndroidConnectionMonitor(ctx, connection.Serial, wait, func() error {
		var loadErr error
		summaries, loadErr = loadPlaylists(options.Source, true, nil)
		if loadErr != nil {
			return loadErr
		}
		for _, playlist := range summaries {
			name := safeName(playlist.Name)
			if existingPlaylistsFolded[portablePathKey(name)] {
				existingPlaylists[name] = true
			}
		}
		selected, loadErr = androidPlaylistSelector(summaries, existingPlaylists)
		if loadErr != nil {
			return loadErr
		}
		playlists, loadErr = loadSyncPlaylists(options.Source, selected, options.Refresh)
		if loadErr != nil {
			return loadErr
		}
		plan, missing, loadErr = makePlan(playlists)
		if loadErr != nil {
			return loadErr
		}
		return validatePlan(plan, playlists)
	})
	if err != nil {
		return err
	}
	// Androidの共有ストレージは、内部ストレージ・SD・USBのいずれも
	// 大文字小文字を区別しない名前解決を行う。findが返す実体の表記と
	// Music.app由来の表記が異なっても、同じファイルとして扱う。
	addCaseInsensitiveInventoryAliases(inventory, androidPlannedRemotePaths(plan, summaries))

	artworkDir, err := os.MkdirTemp("", "music-bridge-android-artwork-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(artworkDir)
	artworkDirs := androidArtworkCandidateDirs(plan, inventory)
	if !options.DryRun && options.Source == "" {
		err := withAndroidConnectionMonitor(ctx, connection.Serial, wait, func() error {
			if exportErr := exportArtworks(playlists, artworkDir, artworkRequests(playlists, plan)); exportErr != nil {
				return exportErr
			}
			var planErr error
			plan, missing, planErr = makePlan(playlists)
			return planErr
		})
		if err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		fmt.Printf("ローカルファイルなし: %d曲\n", len(missing))
	}

	playlistDir, err := os.MkdirTemp("", "music-bridge-android-playlists-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(playlistDir)

	artwork := androidArtworkContent(plan, artworkDirs)
	preliminaryPlaylists, err := androidPlaylistContent(playlists, plan, playlistDir)
	if err != nil {
		return err
	}
	audio := androidAudioContent(plan)
	transferPlan := makeAndroidTransferPlan(
		androidContentInTransferOrder(preliminaryPlaylists, artwork, audio),
		inventory,
	)
	required := transferPlan.requiredBytes()
	free, err := retryAndroidValue(ctx, func() (int64, error) {
		return androidFreeBytes(connection.Serial(), volume.Path)
	}, wait)
	if err != nil {
		return err
	}
	fmt.Printf("選択プレイリスト: %d件 / 曲: %d曲\n", len(playlists), countTracks(playlists))
	fmt.Printf("新規転送容量: 音源 %s + ジャケ写 %s + プレイリスト %s = %s / 空き容量: %s\n",
		humanBytes(transferPlan.audioBytes), humanBytes(transferPlan.artworkBytes),
		humanBytes(transferPlan.playlistBytes),
		humanBytes(required), humanBytes(free))
	if required > free {
		fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Println("!!! 警告: 容量が不足しています。                  !!!")
		fmt.Printf("!!! 必要容量: %s / 空き容量: %s / 不足: %s !!!\n",
			humanBytes(required), humanBytes(free), humanBytes(required-free))
		fmt.Println("!!! 空き容量に収まる範囲で同期を続行します。      !!!")
		budget := free - transferPlan.playlistBytes
		if budget < 0 {
			budget = 0
		}
		plan = fitAndroidPlan(plan, artwork, inventory, budget)
		artwork = androidArtworkContent(plan, artworkDirs)
	}
	playlistsContent, err := androidPlaylistContent(playlists, plan, playlistDir)
	if err != nil {
		return err
	}
	audio = androidAudioContent(plan)
	transferPlan = makeAndroidTransferPlan(
		androidContentInTransferOrder(playlistsContent, artwork, audio),
		inventory,
	)
	allContent := transferPlan.content

	desired := map[string]bool{}
	for _, item := range allContent {
		desired[item.Relative] = true
	}
	for _, item := range plan {
		artworkRelative := path.Join(
			androidVisiblePath(filepath.ToSlash(filepath.Dir(item.Relative))),
			"AlbumArt.jpg",
		)
		if _, exists := inventory[artworkRelative]; exists {
			desired[artworkRelative] = true
		}
	}
	managed, err := retryAndroidValue(ctx, func() ([]string, error) {
		return loadAndroidManagedPaths(connection.Serial(), root)
	}, wait)
	if err != nil {
		return err
	}
	managed = alignCaseInsensitiveManagedPaths(managed, desired)
	stale := androidStalePaths(managed, desired)
	if _, libraryIndexed := inventory[libraryManifestMarker]; !libraryIndexed {
		stale = append(stale, staleLegacyAndroidLibrary(inventory, desired, true)...)
	}
	stale = append(stale, staleAndroidPlaylists(summaries, selected, inventory)...)
	stale = append(stale, staleAndroidPartials(inventory, desired)...)
	stale = append(stale, staleAndroidTemporaryFiles(inventory)...)
	stale = uniqueStrings(stale)
	for _, warning := range androidDeletionWarningLines(summarizeAndroidDeletions(stale)) {
		fmt.Println(warning)
	}

	if options.DryRun {
		fmt.Printf("dry-run: Androidへ転送予定 %dファイル / 削除予定 %dファイル\n",
			len(transferPlan.pending), len(stale))
		return nil
	}
	backend := adbAndroidBackend{serial: connection.Serial, root: root}
	contentChanged := len(transferPlan.pending) > 0
	if _, libraryIndexed := inventory[libraryManifestMarker]; !libraryIndexed {
		contentChanged = true
	}
	postTransfer := planAndroidPostTransfer(stale, managed, desired, inventory, contentChanged)
	started := time.Now()
	if len(transferPlan.pending) > 0 {
		if err := retryAndroidOperation(ctx, func() error {
			return saveAndroidPendingPaths(connection.Serial(), root, managed, desired)
		}, wait); err != nil {
			return err
		}
	}
	if err := transferAndroidContent(ctx, backend, root, transferPlan, wait); err != nil {
		return err
	}
	if len(stale) > 0 {
		fmt.Printf("Android後処理: 不要ファイルを削除中... 0/%d", len(stale))
		for index, relative := range stale {
			remotePath := path.Join(root, relative)
			if err := retryAndroidOperation(ctx, func() error {
				return backend.Remove(remotePath)
			}, wait); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			fmt.Printf("\033[2K\rAndroid後処理: 不要ファイルを削除中... %d/%d", index+1, len(stale))
		}
		fmt.Println()
	}
	if postTransfer.RemoveEmptyDirs {
		fmt.Print("Android後処理: 空ディレクトリを整理中...")
		if err := retryAndroidOperation(ctx, func() error {
			return removeAndroidEmptyDirs(connection.Serial(), root)
		}, wait); err != nil {
			fmt.Print("\033[2K\r")
			return err
		}
		fmt.Print("\033[2K\rAndroid後処理: 空ディレクトリを整理完了\n")
	}
	if postTransfer.SaveManifest {
		manifestPaths := make([]string, 0, len(desired))
		for relative := range desired {
			manifestPaths = append(manifestPaths, relative)
		}
		fmt.Print("Android後処理: 管理情報を保存中...")
		if err := retryAndroidOperation(ctx, func() error {
			return saveAndroidManifest(connection.Serial(), root, manifestPaths)
		}, wait); err != nil {
			fmt.Print("\033[2K\r")
			return err
		}
		fmt.Print("\033[2K\rAndroid後処理: 管理情報を保存完了\n")
	}
	fmt.Printf("Android転送時間: %s\n同期完了: %dプレイリスト\n",
		time.Since(started).Round(time.Second), len(playlists))
	return nil
}

func androidContentInTransferOrder(
	playlists, artwork, audio []androidContent,
) []androidContent {
	result := make([]androidContent, 0, len(playlists)+len(artwork)+len(audio))
	result = append(result, playlists...)
	result = append(result, artwork...)
	result = append(result, audio...)
	return result
}

type androidPostTransferPlan struct {
	RemoveEmptyDirs bool
	SaveManifest    bool
}

func planAndroidPostTransfer(
	stale, managed []string,
	desired map[string]bool,
	inventory map[string]androidFileState,
	contentChanged bool,
) androidPostTransferPlan {
	manifestCurrent := len(managed) == len(desired)
	if manifestCurrent {
		for _, relative := range managed {
			if !desired[relative] {
				manifestCurrent = false
				break
			}
		}
	}
	_, hasPendingManifest := inventory[androidPendingManifest]
	return androidPostTransferPlan{
		RemoveEmptyDirs: len(stale) > 0,
		SaveManifest:    contentChanged || len(stale) > 0 || !manifestCurrent || hasPendingManifest,
	}
}

func loadAndroidInventoryWithProgress(
	ctx context.Context,
	serial androidSerial,
	root string,
	wait androidReconnectWait,
) (map[string]androidFileState, error) {
	started := time.Now()
	fmt.Print("Android上の既存ファイルを確認中... 0ファイル")
	lastDraw := time.Now()
	finalCount := 0
	inventory, err := retryAndroidValue(ctx, func() (map[string]androidFileState, error) {
		result, count, inventoryErr := androidInventoryWithProgress(serial(), root, func(count int) {
			finalCount = count
			if count%100 == 0 || time.Since(lastDraw) >= time.Second {
				fmt.Printf("\033[2K\rAndroid上の既存ファイルを確認中... %dファイル", count)
				lastDraw = time.Now()
			}
		})
		if inventoryErr == nil {
			finalCount = count
		}
		return result, inventoryErr
	}, wait)
	fmt.Print("\033[2K\r")
	if err != nil {
		return nil, err
	}
	fmt.Printf("Android上の既存ファイルを確認完了（%dファイル、%s）\n",
		finalCount, time.Since(started).Round(time.Second))
	return inventory, nil
}

func initializeAndroidTarget(
	ctx context.Context,
	serial androidSerial,
	root string,
	initTarget, dryRun bool,
	wait androidReconnectWait,
) error {
	markerPath := path.Join(root, marker)
	exists, err := retryAndroidValue(ctx, func() (bool, error) {
		out, commandErr := adbShell(serial(), "if [ -f "+shellQuote(markerPath)+" ]; then echo yes; else echo no; fi")
		if commandErr != nil {
			return false, adbError("Android同期先を確認できませんでした", out, commandErr)
		}
		return strings.TrimSpace(string(out)) == "yes", nil
	}, wait)
	if err != nil || exists {
		return err
	}
	if !initTarget && !confirmDefaultYes(fmt.Sprintf("%sを初期化しますか？ [Y/n] ", root)) {
		return fmt.Errorf("同期先の初期化をキャンセルしました")
	}
	if dryRun {
		return nil
	}
	if err := retryAndroidOperation(ctx, func() error {
		out, commandErr := adbShell(serial(), "mkdir -p "+shellQuote(root))
		if commandErr != nil {
			return adbError("Android同期先を初期化できませんでした", out, commandErr)
		}
		return nil
	}, wait); err != nil {
		return err
	}
	return retryAndroidOperation(ctx, func() error {
		return adbWrite(serial(), markerPath, []byte("Music Bridge target\n"))
	}, wait)
}

func lockAndroidTarget(deviceIdentity, volumeIdentity string) (func(), error) {
	cache, err := androidCacheDir()
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(deviceIdentity+"\x00"+volumeIdentity)))
	lockRoot := filepath.Join(cache, "Music Bridge", "android-locks", key)
	if err := os.MkdirAll(lockRoot, 0700); err != nil {
		return nil, err
	}
	return lockTarget(lockRoot)
}

func retryAndroidValue[T any](
	ctx context.Context,
	operation func() (T, error),
	wait androidReconnectWait,
) (T, error) {
	var zero T
	for {
		value, err := operation()
		if err == nil {
			return value, nil
		}
		if !isRetryableADBError(err) {
			return zero, err
		}
		if err := wait(ctx, err); err != nil {
			return zero, err
		}
	}
}

func androidArtworkCandidateDirs(plan []Planned, inventory map[string]androidFileState) map[string]bool {
	result := map[string]bool{}
	for _, item := range plan {
		directory := androidVisiblePath(filepath.ToSlash(filepath.Dir(item.Relative)))
		if _, exists := inventory[path.Join(directory, "AlbumArt.jpg")]; !exists {
			result[directory] = true
		}
	}
	return result
}

func androidPlannedRemotePaths(plan []Planned, playlists []Playlist) []string {
	result := make([]string, 0, len(plan)*2+len(playlists))
	for _, item := range plan {
		relative := androidVisiblePath(filepath.ToSlash(item.Relative))
		result = append(result, relative, path.Join(path.Dir(relative), "AlbumArt.jpg"))
	}
	for _, playlist := range playlists {
		result = append(result, androidVisiblePath(safeName(playlist.Name)+".m3u"))
	}
	return result
}

func addCaseInsensitiveInventoryAliases(inventory map[string]androidFileState, desired []string) {
	folded := make(map[string]androidFileState, len(inventory))
	for relative, state := range inventory {
		folded[portablePathKey(relative)] = state
	}
	for _, relative := range desired {
		if _, exists := inventory[relative]; exists {
			continue
		}
		if state, exists := folded[portablePathKey(relative)]; exists {
			inventory[relative] = state
		}
	}
}

func alignCaseInsensitiveManagedPaths(managed []string, desired map[string]bool) []string {
	desiredByFold := make(map[string]string, len(desired))
	for relative := range desired {
		desiredByFold[portablePathKey(relative)] = relative
	}
	for index, relative := range managed {
		if aligned, exists := desiredByFold[portablePathKey(relative)]; exists {
			managed[index] = aligned
		}
	}
	return uniqueStrings(managed)
}

func androidArtworkContent(plan []Planned, candidates map[string]bool) []androidContent {
	seen := map[string]bool{}
	var result []androidContent
	for _, planned := range plan {
		directory := androidVisiblePath(filepath.ToSlash(filepath.Dir(planned.Relative)))
		if seen[directory] || !candidates[directory] || planned.Track.Artwork == "" {
			continue
		}
		info, err := os.Stat(planned.Track.Artwork)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[directory] = true
		result = append(result, androidContent{
			Source:   planned.Track.Artwork,
			Relative: path.Join(directory, "AlbumArt.jpg"),
			Name:     path.Base(directory),
			Kind:     "ジャケ写",
			Size:     info.Size(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Relative < result[j].Relative })
	return result
}

func androidPlaylistContent(playlists []Playlist, plan []Planned, directory string) ([]androidContent, error) {
	available := map[string]string{}
	for _, item := range plan {
		available[item.Track.Location] = androidVisiblePath(filepath.ToSlash(item.Relative))
	}
	result := make([]androidContent, 0, len(playlists))
	for _, playlist := range playlists {
		relative := androidVisiblePath(safeName(playlist.Name) + ".m3u")
		local := filepath.Join(directory, relative)
		if err := os.WriteFile(local, renderPlaylist(playlist, available), 0644); err != nil {
			return nil, err
		}
		info, err := os.Stat(local)
		if err != nil {
			return nil, err
		}
		result = append(result, androidContent{
			Source: local, Relative: relative, Name: playlist.Name, Kind: "プレイリスト", Size: info.Size(),
		})
	}
	return result, nil
}

func androidAudioContent(plan []Planned) []androidContent {
	result := make([]androidContent, 0, len(plan))
	for _, item := range plan {
		result = append(result, androidContent{
			Source: item.Track.Location, Relative: androidVisiblePath(filepath.ToSlash(item.Relative)),
			Name: item.Track.Name, Kind: "音源", Size: item.Size,
		})
	}
	return result
}

func makeAndroidTransferPlan(
	content []androidContent,
	inventory map[string]androidFileState,
) androidTransferPlan {
	result := androidTransferPlan{content: content}
	const maxLoggedDifferences = 200
	for _, item := range content {
		state, exists := inventory[item.Relative]
		reason := androidFileDifference(item.Source, state, exists)
		if reason == "" {
			continue
		}
		result.pending = append(result.pending, item)
		switch item.Kind {
		case "音源":
			result.audioBytes += item.Size
		case "ジャケ写":
			result.artworkBytes += item.Size
		case "プレイリスト":
			result.playlistBytes += item.Size
		}
		if len(result.pending) <= maxLoggedDifferences {
			logf("android pending %s %q (%s): %s", item.Kind, item.Relative, humanBytes(item.Size), reason)
		}
	}
	if len(result.pending) > maxLoggedDifferences {
		logf("android pending: %d more files omitted", len(result.pending)-maxLoggedDifferences)
	}
	logf(
		"android transfer plan: %d files, audio=%s artwork=%s playlists=%s",
		len(result.pending), humanBytes(result.audioBytes),
		humanBytes(result.artworkBytes), humanBytes(result.playlistBytes),
	)
	return result
}

func (plan androidTransferPlan) requiredBytes() int64 {
	return plan.audioBytes + plan.artworkBytes + plan.playlistBytes
}

func fitAndroidPlan(plan []Planned, artwork []androidContent, inventory map[string]androidFileState, budget int64) []Planned {
	artworkByDir := map[string]androidContent{}
	for _, item := range artwork {
		artworkByDir[path.Dir(item.Relative)] = item
	}
	chargedArtwork := map[string]bool{}
	var result []Planned
	for _, item := range plan {
		relative := androidVisiblePath(filepath.ToSlash(item.Relative))
		cost := int64(0)
		if !sameAndroidFile(item.Track.Location, inventory[relative]) {
			cost += item.Size
		}
		directory := path.Dir(relative)
		art, hasArtwork := artworkByDir[directory]
		if hasArtwork && !chargedArtwork[directory] {
			if !sameAndroidFile(art.Source, inventory[art.Relative]) {
				cost += art.Size
			}
		}
		if cost > budget {
			continue
		}
		result = append(result, item)
		budget -= cost
		if hasArtwork {
			chargedArtwork[directory] = true
		}
	}
	return result
}

func transferAndroidContent(
	ctx context.Context,
	backend androidTransferBackend,
	root string,
	plan androidTransferPlan,
	wait androidReconnectWait,
) error {
	pending := plan.pending
	total := int64(0)
	for _, item := range pending {
		total += item.Size
	}
	if len(pending) == 0 {
		fmt.Println("Android転送: 新規ファイルなし")
		return nil
	}
	started := time.Now()
	var done, transferred int64
	draw := func(index int, item androidContent) {
		percent := float64(done) * 100 / float64(total)
		speedText, etaText := "-", "-"
		elapsed := time.Since(started)
		if transferred > 0 && elapsed > 0 {
			rate := int64(float64(transferred) / elapsed.Seconds())
			if rate > 0 {
				speedText = humanBytes(rate) + "/s"
				etaText = (time.Duration(float64(total-done)/float64(rate)) * time.Second).Round(time.Second).String()
			}
		}
		fmt.Printf("\033[2K\r%s", androidTransferProgressLine(
			etaText, speedText, index, len(pending), percent, done, total, item,
		))
	}
	for index, item := range pending {
		firstProgress := true
		lastOffset := int64(0)
		err := copyAndroidFile(ctx, backend, item.Source, path.Join(root, item.Relative), item.Relative, wait,
			func(offset int64) {
				if firstProgress {
					done += offset
					firstProgress = false
				} else if offset > lastOffset {
					delta := offset - lastOffset
					done += delta
					transferred += delta
				}
				lastOffset = offset
				draw(index, item)
			})
		if err != nil {
			fmt.Print("\033[2K\r")
			return err
		}
	}
	fmt.Print("\033[2K\r")
	fmt.Printf("Android転送完了: %dファイル / %s\n", len(pending), humanBytes(total))
	return nil
}

func androidTransferProgressLine(
	eta, speed string,
	index, count int,
	percent float64,
	done, total int64,
	item androidContent,
) string {
	return fmt.Sprintf(
		"ETA %s | 速度 %s | Android転送中 [%d/%d] %5.1f%% (%s/%s) | %s: %s",
		eta, speed, index+1, count, percent, humanBytes(done), humanBytes(total),
		item.Kind, truncateRunes(item.Name, 20),
	)
}

func staleAndroidPlaylists(all, selected []Playlist, inventory map[string]androidFileState) []string {
	selectedNames := map[string]bool{}
	for _, playlist := range selected {
		selectedNames[portablePathKey(safeName(playlist.Name))] = true
	}
	var stale []string
	for relative := range inventory {
		if strings.Contains(relative, "/") || path.Ext(relative) != ".m3u" || isAppleDoublePath(relative) {
			continue
		}
		name := strings.TrimSuffix(relative, ".m3u")
		if !selectedNames[portablePathKey(name)] {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	return stale
}

func staleLegacyAndroidLibrary(
	inventory map[string]androidFileState,
	desired map[string]bool,
	caseInsensitive bool,
) []string {
	desiredFolded := map[string]bool{}
	if caseInsensitive {
		for relative := range desired {
			desiredFolded[portablePathKey(relative)] = true
		}
	}
	var stale []string
	for relative := range inventory {
		if !strings.HasPrefix(relative, libraryDir+"/") ||
			isAppleDoublePath(relative) ||
			strings.HasSuffix(relative, androidPartialSuffix) ||
			strings.HasSuffix(relative, androidPartialSuffix+androidPartialMetaSuffix) {
			continue
		}
		if !desired[relative] &&
			(!caseInsensitive || !desiredFolded[portablePathKey(relative)]) {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	return stale
}

func staleAndroidTemporaryFiles(inventory map[string]androidFileState) []string {
	candidates := []string{
		legacyArtworkManifestMarker,
		manifest + ".tmp",
		pendingManifest + ".tmp",
		libraryManifestMarker + ".tmp",
	}
	var stale []string
	for _, relative := range candidates {
		if _, exists := inventory[relative]; exists {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	return stale
}

type androidDeletionSummary struct {
	Tracks    int
	Artwork   int
	Playlists int
	Temporary int
}

func summarizeAndroidDeletions(stale []string) androidDeletionSummary {
	var summary androidDeletionSummary
	for _, relative := range stale {
		switch {
		case isAppleDoublePath(relative):
			summary.Temporary++
		case strings.HasPrefix(relative, libraryDir+"/") &&
			path.Base(relative) == albumArtworkFile:
			summary.Artwork++
		case strings.HasPrefix(relative, libraryDir+"/") &&
			(strings.HasSuffix(relative, androidPartialSuffix) ||
				strings.HasSuffix(relative, androidPartialSuffix+androidPartialMetaSuffix)):
			summary.Temporary++
		case strings.HasPrefix(relative, libraryDir+"/"):
			summary.Tracks++
		case !strings.Contains(relative, "/") && path.Ext(relative) == ".m3u":
			summary.Playlists++
		default:
			summary.Temporary++
		}
	}
	return summary
}

func androidDeletionWarningLines(summary androidDeletionSummary) []string {
	var warnings []string
	if summary.Tracks > 0 {
		warnings = append(warnings,
			fmt.Sprintf("警告: 選択対象から外れた曲をAndroidから削除します（%d曲）", summary.Tracks))
	}
	if summary.Artwork > 0 {
		warnings = append(warnings,
			fmt.Sprintf("警告: 不要になったジャケ写をAndroidから削除します（%dファイル）", summary.Artwork))
	}
	if summary.Playlists > 0 {
		warnings = append(warnings,
			fmt.Sprintf("警告: 選択されなかったプレイリストをAndroidから削除します（%d件）", summary.Playlists))
	}
	if summary.Temporary > 0 {
		warnings = append(warnings,
			fmt.Sprintf("警告: 中断された転送ファイルや古い管理情報をAndroidから削除します（%dファイル）",
				summary.Temporary))
	}
	return warnings
}

func staleAndroidPartials(inventory map[string]androidFileState, desired map[string]bool) []string {
	var stale []string
	for relative := range inventory {
		base := strings.TrimSuffix(relative, androidPartialMetaSuffix)
		if !strings.HasSuffix(base, androidPartialSuffix) {
			continue
		}
		destination := strings.TrimSuffix(base, androidPartialSuffix)
		_, finalExists := inventory[destination]
		if !desired[destination] || finalExists {
			stale = append(stale, relative)
		}
	}
	return stale
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
