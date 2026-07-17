package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSafeName(t *testing.T) {
	if got := safeName(" A/B "); got != "AB" {
		t.Fatalf("safeName = %q", got)
	}
	if got := safeName(" "); got != "Unknown" {
		t.Fatalf("empty safeName = %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	for _, test := range []struct {
		value int64
		want  string
	}{
		{3, "3.0 B"},
		{1024, "1.0 KiB"},
		{2 * 1024 * 1024, "2.0 MiB"},
	} {
		if got := humanBytes(test.value); got != test.want {
			t.Errorf("humanBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestValidatePlanRejectsMissingSourceVolume(t *testing.T) {
	playlists := []Playlist{{Name: "P", Tracks: []Track{{Name: "song", Location: "/not-mounted/song.m4a"}}}}
	if err := validatePlan(nil, playlists); err == nil {
		t.Fatal("empty plan with selected tracks must be rejected")
	}
	if err := validatePlan(nil, []Playlist{{Name: "Empty"}}); err != nil {
		t.Fatalf("empty playlist must be allowed: %v", err)
	}
}

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

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("あいうえお", 20); got != "あいうえお" {
		t.Fatalf("short title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてと", 20); got != "あいうえおかきくけこさしすせそたちつてと" {
		t.Fatalf("20-rune title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてとな", 20); got != "あいうえおかきくけこさしすせそたちつてと…" {
		t.Fatalf("long title = %q", got)
	}
}

func TestTransferEstimateUsesCompletedTransferTime(t *testing.T) {
	rate, eta := transferEstimate(300, 100, 10*time.Second)
	if rate != 10 {
		t.Fatalf("rate = %d, want 10", rate)
	}
	if eta != 20*time.Second {
		t.Fatalf("eta = %s, want 20s", eta)
	}
	if rate, eta := transferEstimate(300, 100, 0); rate != 0 || eta != 0 {
		t.Fatalf("zero elapsed estimate = %d, %s; want 0, 0s", rate, eta)
	}
}

func TestFitPlanKeepsSharedExistingFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "song.m4a")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	plan := []Planned{{Track: Track{Location: source}, Relative: "Artist/Album/song.m4a", Size: 4}}
	destination := filepath.Join(dir, plan[0].Relative)
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
	if got := fitPlan(plan, dir, 1); !reflect.DeepEqual(got, plan) {
		t.Fatalf("existing file should fit without consuming free space: %#v", got)
	}
}

func TestStaleAudioUsesManifestAndSelectedUnion(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Old/Album/old.m4a")
	keep := filepath.Join(dir, "Artist/Album/keep.m4a")
	for _, path := range []string{old, keep} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("song"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifest(dir, []Planned{{Relative: "Old/Album/old.m4a"}, {Relative: "Artist/Album/keep.m4a"}}); err != nil {
		t.Fatal(err)
	}
	stale, size := staleAudio([]Planned{{Relative: "Artist/Album/keep.m4a"}}, dir)
	if !reflect.DeepEqual(stale, []string{"Old/Album/old.m4a"}) || size != 4 {
		t.Fatalf("staleAudio = %#v, %d", stale, size)
	}
}

func TestArtworkRequestsSkipExistingAlbumArt(t *testing.T) {
	dir := t.TempDir()
	playlists := []Playlist{{Name: "P", Tracks: []Track{
		{Location: "/source/a.m4a"},
		{Location: "/source/b.m4a"},
		{Location: "/source/c.m4a"},
	}}}
	plan := []Planned{
		{Track: playlists[0].Tracks[0], Relative: "Artist/Existing/a.m4a"},
		{Track: playlists[0].Tracks[1], Relative: "Artist/Existing/b.m4a"},
		{Track: playlists[0].Tracks[2], Relative: "Artist/New/c.m4a"},
	}
	existingArt := filepath.Join(dir, "Artist", "Existing", "AlbumArt.jpg")
	if err := os.MkdirAll(filepath.Dir(existingArt), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingArt, []byte("art"), 0644); err != nil {
		t.Fatal(err)
	}
	want := []artworkRequest{{playlistIndex: 1, trackIndex: 3}}
	if got := artworkRequests(playlists, plan, artworkCandidateDirs(plan, dir)); !reflect.DeepEqual(got, want) {
		t.Fatalf("artworkRequests = %#v, want %#v", got, want)
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
	if got, want := loadManifest(dir), []string{"Library/Artist/Album/song.m4a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
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

func TestArtworkBytesCountsOnlyNewAlbumArt(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "art.jpg")
	if err := os.WriteFile(art, []byte("artwork"), 0644); err != nil {
		t.Fatal(err)
	}
	plan := []Planned{
		{Track: Track{Artwork: art}, Relative: "Library/A/One/a.m4a"},
		{Track: Track{Artwork: art}, Relative: "Library/A/One/b.m4a"},
		{Track: Track{Artwork: art}, Relative: "Library/B/Two/c.m4a"},
	}
	if got, err := artworkBytes(plan, dir); err != nil || got != 14 {
		t.Fatalf("artworkBytes = %d, %v; want 14, nil", got, err)
	}
	existing := filepath.Join(dir, "Library", "A", "One", "AlbumArt.jpg")
	if err := os.MkdirAll(filepath.Dir(existing), 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(art, existing, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := artworkBytes(plan, dir); err != nil || got != 7 {
		t.Fatalf("artworkBytes with existing art = %d, %v; want 7, nil", got, err)
	}
}

func TestArtworkCandidateDirsUsesDirectoryExistence(t *testing.T) {
	dir := t.TempDir()
	plan := []Planned{{Relative: "Library/Artist/Album/song.m4a"}}
	albumDir := filepath.Join(dir, "Library", "Artist", "Album")
	if got := artworkCandidateDirs(plan, dir); len(got) != 1 {
		t.Fatalf("missing album directory should be a candidate: %#v", got)
	}
	if err := os.MkdirAll(albumDir, 0755); err != nil {
		t.Fatal(err)
	}
	if got := artworkCandidateDirs(plan, dir); len(got) != 0 {
		t.Fatalf("existing album directory should be skipped: %#v", got)
	}
}

func TestWriteArtworksCreatesDirectoryForMissingArtwork(t *testing.T) {
	dir := t.TempDir()
	plan := []Planned{{Relative: "Library/Artist/Album/song.m4a"}}
	artworkDirs := artworkCandidateDirs(plan, dir)
	if err := writeArtworks(plan, dir, false, artworkDirs); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "Library", "Artist", "Album")); err != nil || !info.IsDir() {
		t.Fatalf("missing artwork album directory was not created: %v", err)
	}
}

func TestMakePlanKeepsArtworkPath(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.m4a")
	artwork := filepath.Join(dir, "art.jpg")
	for path := range map[string]string{audio: "audio", artwork: "art"} {
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	playlists := []Playlist{{Tracks: []Track{{
		Name: "Song", AlbumArtist: "Artist", Album: "Album", Location: audio, Artwork: artwork,
	}}}}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(plan) != 1 || plan[0].Track.Artwork != artwork {
		t.Fatalf("plan artwork = %#v, missing = %#v", plan, missing)
	}
}
