package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunAndroidSyncDryRunExercisesCompletePlanningFlow(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "library.json")
	audioFile := filepath.Join(t.TempDir(), "song.m4a")
	if err := os.WriteFile(audioFile, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal([]Playlist{{
		Name: "P",
		Tracks: []Track{{
			Name: "Song", Artist: "Artist", Album: "Album", Location: audioFile,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, source, 0600); err != nil {
		t.Fatal(err)
	}

	previousCacheDir := androidCacheDir
	previousSelector := androidPlaylistSelector
	androidCacheDir = func() (string, error) { return t.TempDir(), nil }
	androidPlaylistSelector = func(playlists []Playlist, _ map[string]bool) ([]Playlist, error) {
		return playlists, nil
	}
	t.Cleanup(func() {
		androidCacheDir = previousCacheDir
		androidPlaylistSelector = previousSelector
	})

	const serial = "adb-test._adb-tls-connect._tcp"
	var commands []string
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			commands = append(commands, joined)
			command := args[len(args)-1]
			switch {
			case joined == "devices -l":
				return []byte("List of devices attached\n" + serial + " device model:Pixel_6a\n"), nil
			case strings.Contains(joined, "shell dumpsys mount"):
				return nil, nil
			case strings.Contains(command, "hardware=$(getprop"):
				return []byte("stable-device-id\nandroid-id\n"), nil
			case strings.Contains(command, marker) && strings.Contains(command, "echo yes"):
				return []byte("yes\n"), nil
			case strings.Contains(command, "-type f -printf"):
				return nil, nil
			case strings.Contains(command, "sha256sum"):
				return nil, nil
			case strings.HasPrefix(command, "df -k"):
				return []byte("/dev/test 100000 1 99999 1% /storage/emulated/0\n"), nil
			case strings.HasPrefix(command, "for f in"):
				return nil, nil
			default:
				t.Fatalf("unexpected adb command: %q", joined)
				return nil, nil
			}
		},
		run: func(io.Reader, ...string) ([]byte, error) {
			t.Fatal("dry-run wrote to Android")
			return nil, nil
		},
	})

	err = runAndroidSync(androidSyncOptions{
		Device:  serial,
		Storage: "primary",
		DryRun:  true,
		Source:  sourceFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"devices -l", "dumpsys mount", "-type f -printf", "sha256sum", "df -k"} {
		found := false
		for _, command := range commands {
			if strings.Contains(command, fragment) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("planning flow did not execute %q: %#v", fragment, commands)
		}
	}
}

func TestRunAndroidSyncUnchangedTargetSkipsAllWrites(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "library.json")
	audioFile := filepath.Join(t.TempDir(), "song.m4a")
	if err := os.WriteFile(audioFile, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	audioInfo, err := os.Stat(audioFile)
	if err != nil {
		t.Fatal(err)
	}
	playlist := Playlist{
		Name: "P",
		Tracks: []Track{{
			Name: "Song", Artist: "Artist", Album: "Album", Location: audioFile,
		}},
	}
	source, err := json.Marshal([]Playlist{playlist})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, source, 0600); err != nil {
		t.Fatal(err)
	}
	remoteAudio := "Library/Artist/Album/song.m4a"
	remoteStoredAudio := "Library/ARTIST/Album/song.m4a"
	playlistData := renderPlaylist(playlist, map[string]string{audioFile: remoteAudio})
	playlistHash := fmt.Sprintf("%x", sha256.Sum256(playlistData))

	previousCacheDir := androidCacheDir
	previousSelector := androidPlaylistSelector
	androidCacheDir = func() (string, error) { return t.TempDir(), nil }
	androidPlaylistSelector = func(playlists []Playlist, _ map[string]bool) ([]Playlist, error) {
		return playlists, nil
	}
	t.Cleanup(func() {
		androidCacheDir = previousCacheDir
		androidPlaylistSelector = previousSelector
	})

	const serial = "adb-test._adb-tls-connect._tcp"
	const root = "/storage/emulated/0/music-bridge"
	inventoryData := fmt.Sprintf(
		"%s/%s\x00%d\x00%d\x00%s/P.m3u\x00%d\x000\x00%s/%s\x000\x000\x00",
		root, remoteStoredAudio, audioInfo.Size(), audioInfo.ModTime().Unix(),
		root, len(playlistData), root, libraryManifestMarker,
	)
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			command := args[len(args)-1]
			switch {
			case joined == "devices -l":
				return []byte("List of devices attached\n" + serial + " device model:Pixel_6a\n"), nil
			case strings.Contains(joined, "shell dumpsys mount"):
				return nil, nil
			case strings.Contains(command, "hardware=$(getprop"):
				return []byte("stable-device-id\n"), nil
			case strings.Contains(command, marker) && strings.Contains(command, "echo yes"):
				return []byte("yes\n"), nil
			case strings.Contains(command, "-type f -printf"):
				return []byte(inventoryData), nil
			case strings.Contains(command, "sha256sum"):
				return []byte(root + "/P.m3u\x00" + playlistHash + "\x00"), nil
			case strings.HasPrefix(command, "df -k"):
				return []byte("/dev/test 100000 1 99999 1% /storage/emulated/0\n"), nil
			case strings.HasPrefix(command, "for f in"):
				return []byte("[\n  \"Library/ARTIST/Album/song.m4a\",\n  \"P.m3u\"\n]\n"), nil
			default:
				t.Fatalf("unexpected adb command: %q", joined)
				return nil, nil
			}
		},
		run: func(io.Reader, ...string) ([]byte, error) {
			t.Fatal("unchanged target was written")
			return nil, nil
		},
	})

	err = runAndroidSync(androidSyncOptions{
		Device: serial, Storage: "primary", Source: sourceFile,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAndroidArtworkCandidateDirsUsesRemoteArtworkExistence(t *testing.T) {
	plan := []Planned{
		{Relative: "Library/A/One/1.m4a"},
		{Relative: "Library/A/Two/2.m4a"},
	}
	got := androidArtworkCandidateDirs(plan, map[string]androidFileState{
		"Library/A/One/AlbumArt.jpg": {},
	})
	if !reflect.DeepEqual(got, map[string]bool{"Library/A/Two": true}) {
		t.Fatalf("candidates=%#v", got)
	}
}

func TestFitAndroidPlanIncludesExistingAndChargesArtworkOnce(t *testing.T) {
	dir := t.TempDir()
	audioA := filepath.Join(dir, "a")
	audioB := filepath.Join(dir, "b")
	art := filepath.Join(dir, "art")
	stamp := time.Unix(1700000000, 0)
	for path, data := range map[string][]byte{audioA: []byte("aaaa"), audioB: []byte("bbbb"), art: []byte("aa")} {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	plan := []Planned{
		{Track: Track{Location: audioA}, Relative: "Library/A/Album/a", Size: 4},
		{Track: Track{Location: audioB}, Relative: "Library/A/Album/b", Size: 4},
	}
	artwork := []androidContent{{Source: art, Relative: "Library/A/Album/AlbumArt.jpg", Size: 2}}
	inventory := map[string]androidFileState{
		"Library/A/Album/a": {Size: 4, ModTime: stamp.Unix()},
	}
	got := fitAndroidPlan(plan, artwork, inventory, 6)
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("fit=%#v", got)
	}
	if got := fitAndroidPlan(plan, artwork, inventory, 5); !reflect.DeepEqual(got, plan[:1]) {
		t.Fatalf("fit with short budget=%#v", got)
	}
}

func TestAndroidPlaylistContentUsesFittedPlan(t *testing.T) {
	dir := t.TempDir()
	playlists := []Playlist{{Name: "P", Tracks: []Track{{Location: "/one"}, {Location: "/two"}}}}
	plan := []Planned{{Track: Track{Location: "/one"}, Relative: "Library/A/one"}}
	content, err := androidPlaylistContent(playlists, plan, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(content[0].Source)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "\xef\xbb\xbf#EXTM3U\nLibrary/A/one\n"; got != want {
		t.Fatalf("playlist=%q", got)
	}
}

func TestAndroidContentPlanningHelpers(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.m4a")
	art := filepath.Join(dir, "art.jpg")
	stamp := time.Unix(1700000000, 0)
	for file, data := range map[string][]byte{audio: []byte("audio"), art: []byte("art")} {
		if err := os.WriteFile(file, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	plan := []Planned{{
		Track:    Track{Name: "Song", Location: audio, Artwork: art},
		Relative: "Library/Artist/Album/song.m4a",
		Size:     5,
	}}
	artwork := androidArtworkContent(plan, map[string]bool{"Library/Artist/Album": true})
	if len(artwork) != 1 || artwork[0].Relative != "Library/Artist/Album/AlbumArt.jpg" || artwork[0].Size != 3 {
		t.Fatalf("artwork=%#v", artwork)
	}
	audioContent := androidAudioContent(plan)
	if len(audioContent) != 1 || audioContent[0].Source != audio || audioContent[0].Kind != "音源" {
		t.Fatalf("audio=%#v", audioContent)
	}
	transfers := makeAndroidTransferPlan(
		androidContentInTransferOrder(nil, artwork, audioContent),
		nil,
	)
	if transfers.audioBytes != 5 || transfers.artworkBytes != 3 || transfers.requiredBytes() != 8 {
		t.Fatalf("transfer plan=%#v", transfers)
	}
	inventory := map[string]androidFileState{
		"Library/Artist/Album/song.m4a":     {Size: 5, ModTime: stamp.Unix()},
		"Library/Artist/Album/AlbumArt.jpg": {Size: 3, ModTime: stamp.Unix()},
	}
	transfers = makeAndroidTransferPlan(
		androidContentInTransferOrder(nil, artwork, audioContent),
		inventory,
	)
	if len(transfers.pending) != 0 || transfers.requiredBytes() != 0 {
		t.Fatalf("matching Android files were counted as pending: %#v", transfers)
	}
}

func TestAndroidContentTransferOrderPlacesPlaylistAndArtworkBeforeAudio(t *testing.T) {
	playlists := []androidContent{{Kind: "プレイリスト", Relative: "P.m3u"}}
	artwork := []androidContent{{Kind: "ジャケ写", Relative: "Library/A/AlbumArt.jpg"}}
	audio := []androidContent{{Kind: "音源", Relative: "Library/A/song.m4a"}}
	got := androidContentInTransferOrder(playlists, artwork, audio)
	want := []androidContent{playlists[0], artwork[0], audio[0]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transfer order=%#v, want %#v", got, want)
	}
}

func TestAndroidPlanningMatchesMacExFATVisiblePath(t *testing.T) {
	source := filepath.Join(t.TempDir(), "song.m4a")
	if err := os.WriteFile(source, []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(source, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	plan := []Planned{{
		Track:    Track{Location: source},
		Relative: `Library/Artist:Name/Album "BEST"/song?.m4a`,
		Size:     5,
	}}
	visible := "Library/Artist\uf022Name/Album \uf020BEST\uf020/song\uf025.m4a"
	inventory := map[string]androidFileState{
		visible: {Size: 5, ModTime: stamp.Unix()},
	}
	if got := androidAudioContent(plan); len(got) != 1 || got[0].Relative != visible {
		t.Fatalf("audio content=%#v", got)
	}
	if got := makeAndroidTransferPlan(androidAudioContent(plan), inventory); got.requiredBytes() != 0 {
		t.Fatalf("existing Mac/exFAT file counted as pending: %#v", got)
	}
	directory := path.Dir(visible)
	if got := androidArtworkCandidateDirs(plan, map[string]androidFileState{
		path.Join(directory, "AlbumArt.jpg"): {},
	}); len(got) != 0 {
		t.Fatalf("existing encoded artwork treated as missing: %#v", got)
	}
}

func TestAndroidPlannedRemotePathsIncludesAudioArtworkAndPlaylists(t *testing.T) {
	plan := []Planned{{
		Relative: `Library/Artist:Name/Album/song?.m4a`,
	}}
	playlists := []Playlist{{Name: "P"}}
	got := androidPlannedRemotePaths(plan, playlists)
	want := []string{
		"Library/Artist\uf022Name/Album/song\uf025.m4a",
		"Library/Artist\uf022Name/Album/AlbumArt.jpg",
		"P.m3u",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remote paths=%#v, want %#v", got, want)
	}
}

func TestAndroidSharedStorageMatchesPathsCaseInsensitively(t *testing.T) {
	inventory := map[string]androidFileState{
		"Library/OCTOTOOL/Album/song.m4a": {Size: 4, ModTime: 10},
	}
	addCaseInsensitiveInventoryAliases(inventory, []string{"Library/Octotool/Album/song.m4a"})
	if got, exists := inventory["Library/Octotool/Album/song.m4a"]; !exists || got.Size != 4 {
		t.Fatalf("inventory alias=%#v, exists=%v", got, exists)
	}

	desired := map[string]bool{"Library/Octotool/Album/song.m4a": true}
	managed := alignCaseInsensitiveManagedPaths(
		[]string{"Library/OCTOTOOL/Album/song.m4a"},
		desired,
	)
	if !reflect.DeepEqual(managed, []string{"Library/Octotool/Album/song.m4a"}) {
		t.Fatalf("managed paths=%#v", managed)
	}
	if stale := androidStalePaths(managed, desired); len(stale) != 0 {
		t.Fatalf("case-only path treated as stale: %#v", stale)
	}
}

func TestRetryAndroidValue(t *testing.T) {
	calls, waits := 0, 0
	got, err := retryAndroidValue(context.Background(), func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("device offline")
		}
		return 42, nil
	}, func(context.Context, error) error {
		waits++
		return nil
	})
	if err != nil || got != 42 || calls != 3 || waits != 2 {
		t.Fatalf("got=%d err=%v calls=%d waits=%d", got, err, calls, waits)
	}
}

func TestLoadAndroidInventoryWithProgress(t *testing.T) {
	root := "/storage/SD/music-bridge"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		command := args[len(args)-1]
		switch {
		case strings.Contains(command, "-type f -printf"):
			return []byte(root + "/Library/song.m4a\x004\x001700000000.5\x00"), nil
		case strings.Contains(command, "sha256sum"):
			return nil, nil
		default:
			t.Fatalf("unexpected command: %q", command)
			return nil, nil
		}
	}})
	inventory, err := loadAndroidInventoryWithProgress(
		context.Background(),
		fixedAndroidSerial("device:5555"),
		root,
		func(context.Context, error) error {
			t.Fatal("reconnect wait was called")
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := inventory["Library/song.m4a"]; got != (androidFileState{Size: 4, ModTime: 1700000000}) {
		t.Fatalf("inventory entry=%#v", got)
	}
}

func TestInitializeAndroidTargetWithInitFlag(t *testing.T) {
	writes := 0
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			if strings.Contains(args[len(args)-1], "echo yes") {
				return []byte("no\n"), nil
			}
			return nil, nil
		},
		run: func(input io.Reader, args ...string) ([]byte, error) {
			writes++
			_, _ = io.ReadAll(input)
			return nil, nil
		},
	})
	if err := initializeAndroidTarget(context.Background(), fixedAndroidSerial("device:5555"), "/storage/SD/music-bridge",
		true, false, func(context.Context, error) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("marker writes=%d", writes)
	}
}

func TestInitializeAndroidTargetDryRunDoesNotWrite(t *testing.T) {
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			if strings.Contains(args[len(args)-1], "echo yes") {
				return []byte("no\n"), nil
			}
			t.Fatalf("unexpected write command: %#v", args)
			return nil, nil
		},
		run: func(io.Reader, ...string) ([]byte, error) {
			t.Fatal("dry-run wrote target marker")
			return nil, nil
		},
	})
	err := initializeAndroidTarget(
		context.Background(),
		fixedAndroidSerial("device:5555"),
		"/storage/SD/music-bridge",
		true,
		true,
		func(context.Context, error) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransferAndroidContentCopiesOnlyPendingItems(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "song")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAndroidBackend{files: map[string][]byte{}, signatures: map[string]string{}}
	content := []androidContent{{Source: source, Relative: "Library/song", Name: "Song", Kind: "音源", Size: 4}}
	transferPlan := makeAndroidTransferPlan(content, nil)
	if err := transferAndroidContent(context.Background(), fake, "/remote", transferPlan,
		func(context.Context, error) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := string(fake.files["/remote/Library/song"]); got != "song" {
		t.Fatalf("copied=%q", got)
	}
	info, _ := os.Stat(source)
	inventory := map[string]androidFileState{"Library/song": {Size: 4, ModTime: info.ModTime().Unix()}}
	transferPlan = makeAndroidTransferPlan(content, inventory)
	if err := transferAndroidContent(context.Background(), fake, "/remote", transferPlan,
		func(context.Context, error) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestAndroidTransferProgressLineHasNoActivitySpinner(t *testing.T) {
	line := androidTransferProgressLine(
		"1m2s",
		"5.1 MiB/s",
		2546,
		7041,
		33.1,
		12<<30,
		36<<30,
		androidContent{Kind: "音源", Name: "光遊走子"},
	)
	if strings.Contains(line, " | 転送中 ") {
		t.Fatalf("progress line contains activity spinner: %q", line)
	}
	if !strings.HasSuffix(line, "| 音源: 光遊走子") {
		t.Fatalf("progress line=%q", line)
	}
}

func TestPlanAndroidPostTransferSkipsUnchangedTargetWork(t *testing.T) {
	desired := map[string]bool{"P.m3u": true, "Library/song.m4a": true}
	managed := []string{"Library/song.m4a", "P.m3u"}
	got := planAndroidPostTransfer(nil, managed, desired, nil, false)
	if got.RemoveEmptyDirs || got.SaveManifest {
		t.Fatalf("unchanged post-transfer plan=%#v", got)
	}
}

func TestPlanAndroidPostTransferRunsOnlyRequiredWork(t *testing.T) {
	desired := map[string]bool{"P.m3u": true}
	tests := []struct {
		name           string
		stale          []string
		managed        []string
		inventory      map[string]androidFileState
		contentChanged bool
		want           androidPostTransferPlan
	}{
		{
			name:  "stale file",
			stale: []string{"Library/old.m4a"}, managed: []string{"Library/old.m4a"},
			want: androidPostTransferPlan{RemoveEmptyDirs: true, SaveManifest: true},
		},
		{
			name: "new content", managed: []string{"P.m3u"}, contentChanged: true,
			want: androidPostTransferPlan{SaveManifest: true},
		},
		{
			name: "manifest differs", managed: nil,
			want: androidPostTransferPlan{SaveManifest: true},
		},
		{
			name: "pending journal", managed: []string{"P.m3u"},
			inventory: map[string]androidFileState{androidPendingManifest: {}},
			want:      androidPostTransferPlan{SaveManifest: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := planAndroidPostTransfer(
				test.stale, test.managed, desired, test.inventory, test.contentChanged,
			)
			if got != test.want {
				t.Fatalf("post-transfer plan=%#v, want %#v", got, test.want)
			}
		})
	}
}

func TestAndroidTransferPlanReturnsOnlyDifferences(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same")
	different := filepath.Join(dir, "different")
	stamp := time.Unix(1700000000, 0)
	for _, file := range []string{same, different} {
		if err := os.WriteFile(file, []byte("song"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	content := []androidContent{
		{Source: same, Relative: "Library/same", Kind: "音源", Size: 4},
		{Source: different, Relative: "Library/different", Kind: "音源", Size: 4},
	}
	inventory := map[string]androidFileState{
		"Library/same":      {Size: 4, ModTime: stamp.Unix()},
		"Library/different": {Size: 3, ModTime: stamp.Unix()},
	}
	got := makeAndroidTransferPlan(content, inventory)
	if !reflect.DeepEqual(got.pending, content[1:]) || got.audioBytes != 4 || got.requiredBytes() != 4 {
		t.Fatalf("transfer plan=%#v, want only %#v", got, content[1:])
	}
}

func TestStaleAndroidPlaylistsAndUniqueStrings(t *testing.T) {
	all := []Playlist{{Name: "Keep"}, {Name: "Remove"}}
	selected := all[:1]
	inventory := map[string]androidFileState{
		"KEEP.m3u": {}, "Remove.m3u": {}, "DeletedFromMusic.m3u": {},
		"Library/Album/list.m3u": {},
	}
	got := staleAndroidPlaylists(all, selected, inventory)
	if want := []string{"DeletedFromMusic.m3u", "Remove.m3u"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale=%#v", got)
	}
	if got := uniqueStrings([]string{"B", "A", "B", ""}); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("unique=%#v", got)
	}
}

func TestExternalAndroidComparisonNormalizesDriveUnicodeNames(t *testing.T) {
	decomposedTrack := "Library/あとか\u3099たり/曲.m4a"
	composedTrack := "Library/あとがたり/曲.m4a"
	state := androidFileState{Size: 123}
	inventory := map[string]androidFileState{
		decomposedTrack:     state,
		"あとか\u3099たり.m3u":   {},
		"._あとか\u3099たり.m3u": {},
	}

	addCaseInsensitiveInventoryAliases(inventory, []string{composedTrack})
	if got := inventory[composedTrack]; got != state {
		t.Fatalf("NFD drive path did not alias to NFC Android path: %#v", got)
	}
	managed := alignCaseInsensitiveManagedPaths(
		[]string{decomposedTrack},
		map[string]bool{composedTrack: true},
	)
	if want := []string{composedTrack}; !reflect.DeepEqual(managed, want) {
		t.Fatalf("normalized managed paths=%#v, want %#v", managed, want)
	}
	if stale := staleLegacyAndroidLibrary(
		map[string]androidFileState{decomposedTrack: state},
		map[string]bool{composedTrack: true},
		true,
	); len(stale) != 0 {
		t.Fatalf("NFD drive track treated as stale on Android: %#v", stale)
	}
	if stale := staleAndroidPlaylists(
		nil,
		[]Playlist{{Name: "あとがたり"}},
		inventory,
	); len(stale) != 0 {
		t.Fatalf("NFD drive playlist treated as stale on Android: %#v", stale)
	}
}

func TestSelectedAlbumKeepsExistingAndroidArtworkInDesiredSet(t *testing.T) {
	plan := []Planned{{Relative: "Library/Artist/Album/song.m4a"}}
	inventory := map[string]androidFileState{
		"Library/Artist/Album/song.m4a":                                                     {},
		"Library/Artist/Album/AlbumArt.jpg":                                                 {},
		"Library/Artist/Album/resume.m4a" + androidPartialSuffix:                            {},
		"Library/Artist/Album/resume.m4a" + androidPartialSuffix + androidPartialMetaSuffix: {},
		"Library/Removed/Album/AlbumArt.jpg":                                                {},
		"Library/Removed/Album/old-song.m4a":                                                {},
		"Library/Unmanaged/file-not-artwork.jpg":                                            {},
	}
	desired := map[string]bool{"Library/Artist/Album/song.m4a": true, "Library/Artist/Album/resume.m4a": true}
	for _, item := range plan {
		artworkRelative := path.Join(filepath.ToSlash(filepath.Dir(item.Relative)), "AlbumArt.jpg")
		if _, exists := inventory[artworkRelative]; exists {
			desired[artworkRelative] = true
		}
	}
	if !desired["Library/Artist/Album/AlbumArt.jpg"] {
		t.Fatal("existing artwork for a selected album was not retained")
	}
	if got, want := staleLegacyAndroidLibrary(inventory, desired, false), []string{
		"Library/Removed/Album/AlbumArt.jpg",
		"Library/Removed/Album/old-song.m4a",
		"Library/Unmanaged/file-not-artwork.jpg",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale legacy library=%#v, want %#v", got, want)
	}
}

func TestStaleLegacyAndroidLibraryPreservesCaseVariantOnExternalStorage(t *testing.T) {
	inventory := map[string]androidFileState{
		"Library/artist/album/song.m4a": {},
		"Library/Other/Album/old.m4a":   {},
	}
	desired := map[string]bool{
		"Library/Artist/Album/song.m4a": true,
	}
	got := staleLegacyAndroidLibrary(inventory, desired, true)
	want := []string{"Library/Other/Album/old.m4a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stale external library=%#v, want %#v", got, want)
	}
}

func TestStaleAndroidTemporaryFiles(t *testing.T) {
	inventory := map[string]androidFileState{
		legacyArtworkManifestMarker:       {},
		manifest + ".tmp":                 {},
		pendingManifest + ".tmp":          {},
		libraryManifestMarker + ".tmp":    {},
		"Library/Artist/Album/._song.m4a": {},
		manifest:                          {},
	}
	if got, want := staleAndroidTemporaryFiles(inventory), []string{
		legacyArtworkManifestMarker,
		libraryManifestMarker + ".tmp",
		manifest + ".tmp",
		pendingManifest + ".tmp",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale temporary files=%#v, want %#v", got, want)
	}
}

func TestSummarizeAndroidDeletions(t *testing.T) {
	stale := []string{
		"Library/Artist/Album/song.m4a",
		"Library/Artist/Album/AlbumArt.jpg",
		"Library/Artist/Album/next.m4a" + androidPartialSuffix,
		"Library/Artist/Album/next.m4a" + androidPartialSuffix + androidPartialMetaSuffix,
		"Library/Artist/Album/._song.m4a",
		"Playlist.m3u",
		manifest + ".tmp",
	}
	got := summarizeAndroidDeletions(stale)
	want := androidDeletionSummary{
		Tracks: 1, Artwork: 1, Playlists: 1, Temporary: 4,
	}
	if got != want {
		t.Fatalf("deletion summary=%#v, want %#v", got, want)
	}
	wantWarnings := []string{
		"警告: 選択対象から外れた曲をAndroidから削除します（1曲）",
		"警告: 不要になったジャケ写をAndroidから削除します（1ファイル）",
		"警告: 選択されなかったプレイリストをAndroidから削除します（1件）",
		"警告: 中断された転送ファイルや古い管理情報をAndroidから削除します（4ファイル）",
	}
	if gotWarnings := androidDeletionWarningLines(got); !reflect.DeepEqual(gotWarnings, wantWarnings) {
		t.Fatalf("deletion warnings=%#v, want %#v", gotWarnings, wantWarnings)
	}
}

func TestLockAndroidTargetRejectsSameTargetAndAllowsDifferentVolume(t *testing.T) {
	previous := androidCacheDir
	cache := t.TempDir()
	androidCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { androidCacheDir = previous })

	unlockFirst, err := lockAndroidTarget("device:5555", "sd-1")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFirst()
	if _, err := lockAndroidTarget("device:5555", "sd-1"); err == nil {
		t.Fatal("same Android target was locked twice")
	}
	unlockOther, err := lockAndroidTarget("device:5555", "sd-2")
	if err != nil {
		t.Fatalf("different Android volume was rejected: %v", err)
	}
	unlockOther()
	unlockOtherDevice, err := lockAndroidTarget("other-device", "sd-1")
	if err != nil {
		t.Fatalf("same volume ID on a different Android device was rejected: %v", err)
	}
	unlockOtherDevice()
}

func TestStaleAndroidPartialsKeepsOnlyResumableSelectedFile(t *testing.T) {
	inventory := map[string]androidFileState{
		"Library/keep.m4a" + androidPartialSuffix:                            {},
		"Library/keep.m4a" + androidPartialSuffix + androidPartialMetaSuffix: {},
		"Library/done.m4a":                          {},
		"Library/done.m4a" + androidPartialSuffix:   {},
		"Library/remove.m4a" + androidPartialSuffix: {},
	}
	desired := map[string]bool{"Library/keep.m4a": true, "Library/done.m4a": true}
	got := uniqueStrings(staleAndroidPartials(inventory, desired))
	want := []string{
		"Library/done.m4a" + androidPartialSuffix,
		"Library/remove.m4a" + androidPartialSuffix,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stale partials=%#v, want %#v", got, want)
	}
}
