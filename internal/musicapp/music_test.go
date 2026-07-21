package musicapp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"music-bridge/internal/library"
)

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
	cached := Playlist{Name: "P", Tracks: []library.Track{{Name: "cached"}}}
	if err := savePlaylistCache(path, playlistCache{Version: playlistCacheVersion, Playlists: map[string]cachedPlaylist{"P": {Playlist: cached, Fingerprint: fingerprintHash([]string{"1"})}}}); err != nil {
		t.Fatal(err)
	}
	detailCalls := 0
	playlistDetails = func(string, bool, []string, string) ([]Playlist, error) { detailCalls++; return nil, nil }
	got, err := loadSyncPlaylists("", []Playlist{{Name: "P"}}, false)
	if err != nil || !reflect.DeepEqual(got, []Playlist{cached}) || detailCalls != 0 {
		t.Fatalf("cache hit = %#v, %v, detail calls %d", got, err, detailCalls)
	}
	updated := Playlist{Name: "P", Tracks: []library.Track{{Name: "updated"}}}
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
		return []Playlist{{Name: "P", Tracks: []library.Track{{Name: "fresh"}}}}, nil
	}
	if got, err := loadSyncPlaylists("", []Playlist{{Name: "P"}}, true); err != nil || got[0].Tracks[0].Name != "fresh" {
		t.Fatalf("refresh=%#v,%v", got, err)
	}
}

func TestPlaylistCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Music Bridge", "library-cache.json")
	want := playlistCache{
		Version: playlistCacheVersion,
		Playlists: map[string]cachedPlaylist{
			"P": {Playlist: Playlist{Name: "P", Tracks: []Track{{Name: "Song", Location: "/music/song.m4a"}}}, Fingerprint: "abc"},
		},
	}
	if err := savePlaylistCache(path, want); err != nil {
		t.Fatal(err)
	}
	if got := loadPlaylistCache(path); !reflect.DeepEqual(got, want) {
		t.Fatalf("cache = %#v, want %#v", got, want)
	}
}

func TestFingerprintHash(t *testing.T) {
	if got, want := fingerprintHash([]string{"A", "B"}), fingerprintHash([]string{"A", "B"}); got != want {
		t.Fatalf("same IDs have different hashes: %q != %q", got, want)
	}
	if fingerprintHash([]string{"A", "B"}) == fingerprintHash([]string{"B", "A"}) {
		t.Fatal("track order should affect the fingerprint")
	}
}
