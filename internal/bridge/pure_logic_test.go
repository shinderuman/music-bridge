package bridge

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlanHelpersAndExistingBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.m4a")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	playlists := []Playlist{{Tracks: []Track{{Name: "one", Artist: "Artist", Album: "Album", Location: source}, {Name: "duplicate", Artist: "Artist", Album: "Album", Location: source}, {Name: "missing"}}}}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || !reflect.DeepEqual(missing, []string{"missing"}) || countTracks(playlists) != 3 || totalBytes(plan) != 4 {
		t.Fatalf("plan=%#v missing=%#v", plan, missing)
	}
	if got, err := existingBytes(plan, root); err != nil || got != 4 {
		t.Fatalf("existingBytes=%d,%v", got, err)
	}
	destination := filepath.Join(root, plan[0].Relative)
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(source, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(destination, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got, err := existingBytes(plan, root); err != nil || got != 0 {
		t.Fatalf("existingBytes=%d,%v", got, err)
	}
	if free, err := freeBytes(root); err != nil || free <= 0 {
		t.Fatalf("freeBytes=%d,%v", free, err)
	}
}

func TestMakePlanSkipsNonRegularLocationsAndFitPlanHonorsBudget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.m4a")
	second := filepath.Join(root, "second.m4a")
	if err := os.WriteFile(first, []byte("1111"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("2222"), 0644); err != nil {
		t.Fatal(err)
	}
	playlists := []Playlist{{Tracks: []Track{{Name: "directory", Location: root}, {Name: "first", Artist: "A", Album: "X", Location: first}, {Name: "second", Artist: "A", Album: "X", Location: second}}}}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []string{"directory"}) || len(plan) != 2 {
		t.Fatalf("plan=%#v missing=%#v", plan, missing)
	}
	if got := fitPlan(plan, filepath.Join(root, "target"), 4); !reflect.DeepEqual(got, plan[:1]) {
		t.Fatalf("fitPlan=%#v", got)
	}
}

func TestPlaylistAndTargetFileHelpers(t *testing.T) {
	root := t.TempDir()
	plan := []Planned{{Track: Track{Location: "/source/song.m4a"}, Relative: "Library/A/Album/song.m4a"}}
	playlists := []Playlist{{Name: "Keep", Tracks: []Track{{Location: "/source/song.m4a"}}}, {Name: "Remove"}}
	if err := writePlaylists(playlists, plan, root, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "Keep.m3u"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "\xef\xbb\xbf#EXTM3U\nLibrary/A/Album/song.m4a\n"; got != want {
		t.Fatalf("playlist=%q", got)
	}
	if got := stalePlaylists(playlists, playlists[:1], root); !reflect.DeepEqual(got, []string{filepath.Join(root, "Remove.m3u")}) {
		t.Fatalf("stale=%#v", got)
	}
	if got, want := string(prefixLibraryInM3U([]byte("#EXTM3U\r\nA/B.m4a\r\nLibrary/C.m4a\r\n"))), "#EXTM3U\r\nLibrary/A/B.m4a\r\nLibrary/C.m4a\r\n"; got != want {
		t.Fatalf("prefixed=%q", got)
	}
	if err := saveManifest(root, []Planned{{Relative: "B"}, {Relative: "A"}}); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest(root); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("manifest=%#v", got)
	}
	empty := filepath.Join(root, "empty", "nested")
	keep := filepath.Join(root, "keep")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyDirs(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Fatalf("empty dir remains: %v", err)
	}
}

func TestCopyStageAndBatchHelpers(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.m4a")
	destination := filepath.Join(dir, "destination.m4a")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := copyFile(source, destination, stamp); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "song" {
		t.Fatalf("copy=%q,%v", data, err)
	}
	item := Planned{Track: Track{Location: source}, Relative: "Library/A/song.m4a", Size: 4}
	stage, err := stagePlan([]Planned{item})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if target, err := os.Readlink(filepath.Join(stage, item.Relative)); err != nil || target != source {
		t.Fatalf("stage link=%q,%v", target, err)
	}
	if batchBytes([]Planned{item, {Size: 3}}) != 7 {
		t.Fatal("batchBytes")
	}
}

func TestMusicInputHelpers(t *testing.T) {
	if got, want := sourceArgs("custom.js", true, true, []string{"A", "B"}, "P"), []string{"-l", "JavaScript", "custom.js", "--summary", "--fingerprint", "--progress-prefix", "P", "--playlist", "A", "--playlist", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%#v", got)
	}
	all := []Playlist{{Name: "A"}, {Name: "B"}}
	if got := filterPlaylists(all, []string{"B"}); !reflect.DeepEqual(got, []Playlist{{Name: "B"}}) {
		t.Fatalf("filtered=%#v", got)
	}
	path := filepath.Join(t.TempDir(), "playlists.json")
	if err := os.WriteFile(path, []byte(`[{"name":"A"},{"name":"B"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := loadPlaylists(path, true, []string{"A"}); err != nil || !reflect.DeepEqual(got, []Playlist{{Name: "A"}}) {
		t.Fatalf("loaded=%#v,%v", got, err)
	}
}

func TestFilesystemHelpersHandleDryRunAndInvalidManifest(t *testing.T) {
	root := t.TempDir()
	plan := []Planned{{Track: Track{Location: "/source/song.m4a"}, Relative: "Library/A/Album/song.m4a"}}
	if err := writePlaylists([]Playlist{{Name: "P", Tracks: []Track{{Location: "/source/song.m4a"}}}}, plan, root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "P.m3u")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote playlist: %v", err)
	}
	if err := writeArtworks(plan, root, true, map[string]bool{"Library/A/Album": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Library")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created artwork directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest(root); got != nil {
		t.Fatalf("invalid manifest = %#v, want nil", got)
	}
}

func TestBundledScriptFallsBackToRelativePath(t *testing.T) {
	name := "definitely-not-a-music-bridge-script"
	if got, want := bundledScript(name), filepath.Join("scripts", name); got != want {
		t.Fatalf("bundledScript = %q, want %q", got, want)
	}
}

func TestBundledScriptUsesAdjacentDistributionScript(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "music-bridge")
	script := filepath.Join(root, "scripts", "export.js")
	if err := os.MkdirAll(filepath.Dir(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0644); err != nil {
		t.Fatal(err)
	}
	previous := executablePath
	executablePath = func() (string, error) { return executable, nil }
	t.Cleanup(func() { executablePath = previous })
	want, err := filepath.EvalSymlinks(script)
	if err != nil {
		t.Fatal(err)
	}
	if got := bundledScript("export.js"); got != want {
		t.Fatalf("bundledScript = %q, want %q", got, want)
	}
}

func TestPlaylistCachePath(t *testing.T) {
	path, err := playlistCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join("Music Bridge", "library-cache.json")) {
		t.Fatalf("cache path = %q", path)
	}
}

func TestLoadPlaylistCacheRejectsInvalidAndOldCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := loadPlaylistCache(path); !reflect.DeepEqual(got, playlistCache{}) {
		t.Fatalf("invalid cache = %#v", got)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"playlists":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := loadPlaylistCache(path); !reflect.DeepEqual(got, playlistCache{}) {
		t.Fatalf("old cache = %#v", got)
	}
}

func TestLoadSyncPlaylistsUsesAndUpdatesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	previousPath, previousFingerprints, previousDetails := playlistCacheLocation, playlistFingerprints, playlistDetails
	playlistCacheLocation = func() (string, error) { return path, nil }
	t.Cleanup(func() {
		playlistCacheLocation, playlistFingerprints, playlistDetails = previousPath, previousFingerprints, previousDetails
	})
	playlistFingerprints = func(names []string) ([]playlistFingerprint, error) {
		return []playlistFingerprint{{Name: "P", TrackIDs: []string{"1"}}}, nil
	}
	cached := Playlist{Name: "P", Tracks: []Track{{Name: "cached"}}}
	if err := savePlaylistCache(path, playlistCache{Version: playlistCacheVersion, Playlists: map[string]cachedPlaylist{"P": {Playlist: cached, Fingerprint: fingerprintHash([]string{"1"})}}}); err != nil {
		t.Fatal(err)
	}
	detailCalls := 0
	playlistDetails = func(string, bool, []string, string) ([]Playlist, error) { detailCalls++; return nil, nil }
	got, err := loadSyncPlaylists("", []Playlist{{Name: "P"}}, false)
	if err != nil || !reflect.DeepEqual(got, []Playlist{cached}) || detailCalls != 0 {
		t.Fatalf("cache hit = %#v, %v, detail calls %d", got, err, detailCalls)
	}
	updated := Playlist{Name: "P", Tracks: []Track{{Name: "updated"}}}
	playlistFingerprints = func(names []string) ([]playlistFingerprint, error) {
		return []playlistFingerprint{{Name: "P", TrackIDs: []string{"2"}}}, nil
	}
	playlistDetails = func(string, bool, []string, string) ([]Playlist, error) {
		detailCalls++
		return []Playlist{updated}, nil
	}
	got, err = loadSyncPlaylists("", []Playlist{{Name: "P"}}, false)
	if err != nil || !reflect.DeepEqual(got, []Playlist{updated}) || detailCalls != 1 {
		t.Fatalf("cache update = %#v, %v, detail calls %d", got, err, detailCalls)
	}
	if saved := loadPlaylistCache(path).Playlists["P"]; !reflect.DeepEqual(saved.Playlist, updated) || saved.Fingerprint != fingerprintHash([]string{"2"}) {
		t.Fatalf("saved cache = %#v", saved)
	}
}

func TestLoadSyncPlaylistsRejectsMissingFingerprintAndRefreshes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	previousPath, previousFingerprints, previousDetails := playlistCacheLocation, playlistFingerprints, playlistDetails
	playlistCacheLocation = func() (string, error) { return path, nil }
	t.Cleanup(func() {
		playlistCacheLocation, playlistFingerprints, playlistDetails = previousPath, previousFingerprints, previousDetails
	})
	playlistFingerprints = func([]string) ([]playlistFingerprint, error) { return nil, nil }
	if _, err := loadSyncPlaylists("", []Playlist{{Name: "P"}}, false); err == nil {
		t.Fatal("missing fingerprint succeeded")
	}
	playlistFingerprints = func([]string) ([]playlistFingerprint, error) {
		return []playlistFingerprint{{Name: "P", TrackIDs: []string{"1"}}}, nil
	}
	playlistDetails = func(string, bool, []string, string) ([]Playlist, error) {
		return []Playlist{{Name: "P", Tracks: []Track{{Name: "fresh"}}}}, nil
	}
	if got, err := loadSyncPlaylists("", []Playlist{{Name: "P"}}, true); err != nil || got[0].Tracks[0].Name != "fresh" {
		t.Fatalf("refresh=%#v,%v", got, err)
	}
}
