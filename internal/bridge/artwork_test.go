package bridge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

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

func TestWriteArtworksCopiesAlbumArt(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.jpg")
	if err := os.WriteFile(source, []byte("artwork"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(source, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	plan := []Planned{{Track: Track{Artwork: source}, Relative: "Library/Artist/Album/song.m4a"}}
	dirs := map[string]bool{"Library/Artist/Album": true}
	if err := writeArtworks(plan, root, false, dirs); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "Library/Artist/Album/AlbumArt.jpg")
	if data, err := os.ReadFile(destination); err != nil || string(data) != "artwork" {
		t.Fatalf("artwork = %q, %v", data, err)
	}
	if info, err := os.Stat(destination); err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("artwork modtime = %v, %v", info.ModTime(), err)
	}
}
