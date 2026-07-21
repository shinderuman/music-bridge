package drive

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"music-bridge/internal/layout"
)

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(a, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("same files were not recognized")
	}
	if err := os.WriteFile(b, []byte("diff"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("same metadata should be treated as unchanged")
	}
	if err := os.Chtimes(b, stamp.Add(1500*time.Millisecond), stamp.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("small filesystem timestamp precision differences should be ignored")
	}
	if err := os.Chtimes(b, stamp.Add(3*time.Second), stamp.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if sameFile(a, b) {
		t.Fatal("large timestamp differences should not be ignored")
	}
}

func TestLockTargetRejectsConcurrentSyncForSameTarget(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockTarget(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if _, err := lockTarget(root); err == nil {
		t.Fatal("second lock for the same target succeeded")
	}
	unlock()
	unlockAgain, err := lockTarget(root)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	unlockAgain()
}

func TestLockTargetIsReleasedAfterSIGINT(t *testing.T) {
	root := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestLockTargetSignalHelper$")
	command.Env = append(os.Environ(), "MUSIC_BRIDGE_LOCK_TEST_ROOT="+root)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper output = %q, want locked", line)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("helper unexpectedly exited without SIGINT")
	}
	unlock, err := lockTarget(root)
	if err != nil {
		t.Fatalf("lock remained after SIGINT: %v", err)
	}
	unlock()
}

func TestLockTargetSignalHelper(t *testing.T) {
	root := os.Getenv("MUSIC_BRIDGE_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	unlock, err := lockTarget(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer unlock()
	fmt.Println("locked")
	select {}
}

func TestStaleAudioUsesManifestAndSelectedUnion(t *testing.T) {
	dir := t.TempDir()
	oldRelative := filepath.Join(libraryDir, "Old", "Album", "old.m4a")
	keepRelative := filepath.Join(libraryDir, "Artist", "Album", "keep.m4a")
	old := filepath.Join(dir, oldRelative)
	keep := filepath.Join(dir, keepRelative)
	for _, path := range []string{old, keep} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("song"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifest(dir, []Planned{{Relative: oldRelative}, {Relative: keepRelative}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Keep.m3u"), []byte("#EXTM3U\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveManifestPaths(dir, []string{oldRelative, keepRelative, "Keep.m3u"}); err != nil {
		t.Fatal(err)
	}
	stale, size := staleAudio([]Planned{{Relative: keepRelative}}, dir)
	if !reflect.DeepEqual(stale, []string{oldRelative}) || size != 4 {
		t.Fatalf("staleAudio = %#v, %d", stale, size)
	}
}

func TestStaleManagementFiles(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{
		legacyArtworkManifestMarker,
		manifest + ".tmp",
		pendingManifest + ".tmp",
		libraryManifestMarker + ".tmp",
	} {
		if err := os.WriteFile(filepath.Join(root, relative), []byte("stale"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := staleManagementFiles(root), []string{
		legacyArtworkManifestMarker,
		libraryManifestMarker + ".tmp",
		manifest + ".tmp",
		pendingManifest + ".tmp",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale management files=%#v, want %#v", got, want)
	}
}

func TestAppleDoubleFilesAreMetadataNotAudioOrPlaylists(t *testing.T) {
	root := t.TempDir()
	track := filepath.Join(libraryDir, "Artist", "Album", "song.m4a")
	sidecar := filepath.Join(libraryDir, "Artist", "Album", "._song.m4a")
	for path, contents := range map[string]string{
		track:            "song",
		sidecar:          "metadata",
		"Playlist.m3u":   "#EXTM3U\n",
		"._Playlist.m3u": "metadata",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	audio, _ := staleAudio([]Planned{{Relative: track}}, root)
	if len(audio) != 0 {
		t.Fatalf("AppleDouble was treated as stale audio: %#v", audio)
	}
	playlistPlan, err := makePlaylistSyncPlan([]Playlist{{Name: "Playlist"}}, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(playlistPlan.stale) != 0 {
		t.Fatalf("AppleDouble was treated as a playlist: %#v", playlistPlan.stale)
	}
	if err := saveManifest(root, []Planned{{Relative: track}}); err != nil {
		t.Fatal(err)
	}
	if got, want := loadManifest(root), []string{track, "Playlist.m3u"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest=%#v, want %#v", got, want)
	}
}

func TestStaleLocalContentNormalizesUnicodePaths(t *testing.T) {
	root := t.TempDir()
	decomposedTrack := filepath.Join(libraryDir, "あとか\u3099たり", "曲.m4a")
	composedTrack := filepath.Join(libraryDir, "あとがたり", "曲.m4a")
	decomposedArtwork := filepath.Join(filepath.Dir(decomposedTrack), albumArtworkFile)
	for _, relative := range []string{decomposedTrack, decomposedArtwork} {
		absolute := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifestPaths(root, []string{decomposedTrack, decomposedArtwork}); err != nil {
		t.Fatal(err)
	}
	plan := []Planned{{Relative: composedTrack}}
	if stale, _ := staleAudio(plan, root); len(stale) != 0 {
		t.Fatalf("NFD audio was stale for NFC plan: %#v", stale)
	}
	if stale, _, err := staleArtwork(plan, root); err != nil || len(stale) != 0 {
		t.Fatalf("NFD artwork was stale for NFC plan: %#v, err=%v", stale, err)
	}
}

func TestStaleArtworkMigratesLegacyTargetAndPreservesSelectedAlbums(t *testing.T) {
	root := t.TempDir()
	keepTrack := filepath.Join(libraryDir, "Keep", "Shared Album", "keep.m4a")
	removedTrack := filepath.Join(libraryDir, "Remove", "Old Album", "old.m4a")
	keepArtwork := filepath.Join(filepath.Dir(keepTrack), albumArtworkFile)
	removedArtwork := filepath.Join(filepath.Dir(removedTrack), albumArtworkFile)
	for path, contents := range map[string]string{
		keepTrack:      "keep",
		removedTrack:   "old",
		keepArtwork:    "keep-art",
		removedArtwork: "old-art",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// 旧形式ではmanifest保存前に中断すると、転送済み音源とジャケ写が未登録で残る。
	if err := saveManifestPaths(root, []string{keepTrack}); err != nil {
		t.Fatal(err)
	}

	plan := []Planned{{Relative: keepTrack}}
	if got, _ := staleAudio(plan, root); !reflect.DeepEqual(got, []string{removedTrack}) {
		t.Fatalf("staleAudio = %#v, want %#v", got, []string{removedTrack})
	}
	got, size, err := staleArtwork(plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{removedArtwork}; !reflect.DeepEqual(got, want) {
		t.Fatalf("staleArtwork = %#v, want %#v", got, want)
	}
	if size != int64(len("old-art")) {
		t.Fatalf("stale artwork size = %d, want %d", size, len("old-art"))
	}
}

func TestStaleArtworkUsesIndexedManifestWithoutScanningLibrary(t *testing.T) {
	root := t.TempDir()
	trackedArtwork := filepath.Join(libraryDir, "Tracked", "Album", albumArtworkFile)
	untrackedArtwork := filepath.Join(libraryDir, "Untracked", "Album", albumArtworkFile)
	for _, relative := range []string{trackedArtwork, untrackedArtwork} {
		absolute := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("art"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifestPaths(root, []string{trackedArtwork}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, libraryManifestMarker), []byte("indexed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, size, err := staleArtwork(nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{trackedArtwork}; !reflect.DeepEqual(got, want) {
		t.Fatalf("staleArtwork = %#v, want %#v", got, want)
	}
	if size != 3 {
		t.Fatalf("stale artwork size = %d, want 3", size)
	}
}

func TestSaveManifestIndexesExistingArtwork(t *testing.T) {
	root := t.TempDir()
	track := filepath.Join(libraryDir, "Artist", "Album", "song.m4a")
	artwork := filepath.Join(filepath.Dir(track), albumArtworkFile)
	for path, contents := range map[string]string{track: "song", artwork: "art"} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := saveManifest(root, []Planned{{Relative: track}}); err != nil {
		t.Fatal(err)
	}
	if got, want := loadManifest(root), []string{artwork, track}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, libraryManifestMarker)); err != nil {
		t.Fatalf("library manifest marker missing: %v", err)
	}
}

func TestPendingManifestTracksInterruptedAudioAndArtwork(t *testing.T) {
	root := t.TempDir()
	if err := saveManifest(root, nil); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(libraryDir, "Artist", "Album", "song.m4a")
	artwork := filepath.Join(filepath.Dir(track), albumArtworkFile)
	for path, contents := range map[string]string{track: "song", artwork: "art"} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := savePendingManifest(root, []Planned{{
		Track:    Track{Artwork: filepath.Join(root, artwork)},
		Relative: track,
	}}); err != nil {
		t.Fatal(err)
	}

	if got, _ := staleAudio(nil, root); !reflect.DeepEqual(got, []string{track}) {
		t.Fatalf("stale interrupted audio=%#v, want %#v", got, []string{track})
	}
	gotArtwork, _, err := staleArtwork(nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotArtwork, []string{artwork}) {
		t.Fatalf("stale interrupted artwork=%#v, want %#v", gotArtwork, []string{artwork})
	}
	if err := saveManifest(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, pendingManifest)); !os.IsNotExist(err) {
		t.Fatalf("pending manifest remains after successful manifest save: %v", err)
	}
}

func TestAndroidTargetHasNoLocalContentDiff(t *testing.T) {
	root := t.TempDir()
	sourceRoot := t.TempDir()
	sourceAudio := filepath.Join(sourceRoot, "song?.m4a")
	sourceArtwork := filepath.Join(sourceRoot, "art.jpg")
	if err := os.WriteFile(sourceAudio, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceArtwork, []byte("art"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	for _, path := range []string{sourceAudio, sourceArtwork} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	relative := filepath.Join(libraryDir, "Artist:Name", "Album?", filepath.Base(sourceAudio))
	artworkRelative := filepath.Join(filepath.Dir(relative), albumArtworkFile)
	plan := []Planned{{
		Track: Track{
			Name: "Song", Artist: "Artist:Name", Album: "Album?",
			Location: sourceAudio, Artwork: sourceArtwork,
		},
		Relative: relative,
		Size:     4,
	}}
	for destination, contents := range map[string]string{
		filepath.Join(root, relative):        "song",
		filepath.Join(root, artworkRelative): "art",
	} {
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(destination, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	playlist := Playlist{
		Name:   "Question?Mark",
		Tracks: []Track{{Name: "Song", Location: sourceAudio}},
	}
	playlistName := androidVisiblePath(safeName(playlist.Name) + ".m3u")
	playlistPath := filepath.Join(root, playlistName)
	playlistData := renderPlaylist(playlist, map[string]string{
		sourceAudio: filepath.ToSlash(relative),
	})
	if err := os.WriteFile(playlistPath, playlistData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(playlistPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	androidManifest := []string{
		androidVisiblePath(filepath.ToSlash(relative)),
		androidVisiblePath(filepath.ToSlash(artworkRelative)),
		playlistName,
	}
	sort.Strings(androidManifest)
	if err := saveManifestPaths(root, androidManifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, libraryManifestMarker), nil, 0644); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		t.Fatal(err)
	}

	if transfers := makeAudioTransferPlan(plan, root); transfers.bytes != 0 || len(transfers.items) != 0 {
		t.Fatalf("audio difference=%#v", transfers)
	}
	artworkTransfers, err := makeArtworkTransferPlan(
		plan, root, map[string]bool{filepath.Dir(relative): true},
	)
	if err != nil || artworkTransfers.bytes != 0 || len(artworkTransfers.copies) != 0 {
		t.Fatalf("artwork difference=%#v, err=%v", artworkTransfers, err)
	}
	if stale, _ := staleAudio(plan, root); len(stale) != 0 {
		t.Fatalf("Android audio treated as stale locally: %#v", stale)
	}
	if stale, _, err := staleArtwork(plan, root); err != nil || len(stale) != 0 {
		t.Fatalf("Android artwork treated as stale locally: %#v, err=%v", stale, err)
	}
	playlistPlan, err := makePlaylistSyncPlan([]Playlist{playlist}, plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(playlistPlan.stale) != 0 {
		t.Fatalf("Android playlist treated as stale locally: %#v", playlistPlan.stale)
	}
	if err := playlistPlan.write(false); err != nil {
		t.Fatal(err)
	}
	playlistInfo, err := os.Stat(playlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !playlistInfo.ModTime().Equal(stamp) {
		t.Fatalf("unchanged playlist was rewritten: %v", playlistInfo.ModTime())
	}
	if err := saveManifest(root, plan); err != nil {
		t.Fatal(err)
	}
	manifestAfter, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifestAfter, manifestBefore) {
		t.Fatalf("cross-mode manifest changed:\nbefore=%s\nafter=%s", manifestBefore, manifestAfter)
	}
}

func TestStaleAudioPreservesResumableAndroidPartialForSelectedTrack(t *testing.T) {
	root := t.TempDir()
	if err := saveManifest(root, nil); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(libraryDir, "Artist", "Album", "song.m4a")
	plan := []Planned{{Relative: track}}
	if err := savePendingManifest(root, plan); err != nil {
		t.Fatal(err)
	}
	partial := track + layout.PartialSuffix
	meta := partial + layout.PartialMetadataSuffix
	for _, relative := range []string{partial, meta} {
		absolute := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("partial"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if got, _ := staleAudio(plan, root); len(got) != 0 {
		t.Fatalf("selected resumable partial was stale: %#v", got)
	}
	if got, _ := staleAudio(nil, root); !reflect.DeepEqual(got, []string{partial, meta}) {
		t.Fatalf("deselected partials=%#v, want %#v", got, []string{partial, meta})
	}
}

func TestMigrateLegacyLayout(t *testing.T) {
	dir := t.TempDir()
	legacyTrack := filepath.Join(dir, "Artist", "Album", "song.m4a")
	if err := os.MkdirAll(filepath.Dir(legacyTrack), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyTrack, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	playlistPath := filepath.Join(dir, "Playlist.m3u")
	if err := os.WriteFile(playlistPath, []byte("\xef\xbb\xbf#EXTM3U\nArtist/Album/song.m4a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(dir, []Planned{{Relative: "Artist/Album/song.m4a"}}); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyLayout(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, libraryDir, "Artist", "Album", "song.m4a")); err != nil {
		t.Fatalf("migrated audio missing: %v", err)
	}
	if _, err := os.Stat(legacyTrack); !os.IsNotExist(err) {
		t.Fatalf("legacy audio still exists: %v", err)
	}
	playlist, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(playlist), "\xef\xbb\xbf#EXTM3U\nLibrary/Artist/Album/song.m4a\n"; got != want {
		t.Fatalf("playlist = %q, want %q", got, want)
	}
	if got, want := loadManifest(dir), []string{
		"Library/Artist/Album/song.m4a",
		"Playlist.m3u",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}
