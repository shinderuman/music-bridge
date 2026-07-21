package drive

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestArtworkRequestsIncludeAllPlannedAlbumsForHostCache(t *testing.T) {
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
	want := []artworkRequest{
		{PlaylistIndex: 1, TrackIndex: 1, AlbumKey: "Artist/Existing"},
		{PlaylistIndex: 1, TrackIndex: 2, AlbumKey: "Artist/Existing"},
		{PlaylistIndex: 1, TrackIndex: 3, AlbumKey: "Artist/New"},
	}
	if got := artworkRequests(playlists, plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("artworkRequests = %#v, want %#v", got, want)
	}
}

func TestArtworkTransferPlanCountsExactlyItsCopies(t *testing.T) {
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
	candidates := map[string]bool{
		"Library/A/One": true,
		"Library/B/Two": true,
	}
	transfers, err := makeArtworkTransferPlan(plan, dir, candidates)
	if err != nil || transfers.bytes != 14 || len(transfers.copies) != 2 {
		t.Fatalf("artwork plan = %#v, %v; want 14 bytes and 2 copies", transfers, err)
	}
	existing := filepath.Join(dir, "Library", "A", "One", "AlbumArt.jpg")
	if err := os.MkdirAll(filepath.Dir(existing), 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(art, existing, time.Now()); err != nil {
		t.Fatal(err)
	}
	transfers, err = makeArtworkTransferPlan(plan, dir, candidates)
	if err != nil || transfers.bytes != 7 || len(transfers.copies) != 1 {
		t.Fatalf("artwork plan with existing art = %#v, %v; want 7 bytes and 1 copy", transfers, err)
	}
}

func TestArtworkTransferPlanDoesNotCountExistingArtWithDifferentTimestamp(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.jpg")
	destination := filepath.Join(root, "Library/Artist/Album/AlbumArt.jpg")
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{source: "new artwork", destination: "old artwork"} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	plan := []Planned{{Track: Track{Artwork: source}, Relative: "Library/Artist/Album/song.m4a"}}
	transfers, err := makeArtworkTransferPlan(plan, root, map[string]bool{"Library/Artist/Album": true})
	if err != nil {
		t.Fatal(err)
	}
	if transfers.bytes != 0 || len(transfers.copies) != 0 {
		t.Fatalf("existing artwork was scheduled: %#v", transfers)
	}
}

func TestArtworkCandidateDirsUsesAlbumArtExistence(t *testing.T) {
	dir := t.TempDir()
	plan := []Planned{{Relative: "Library/Artist/Album/song.m4a"}}
	albumDir := filepath.Join(dir, "Library", "Artist", "Album")
	if got := artworkCandidateDirs(plan, dir); len(got) != 1 {
		t.Fatalf("missing album directory should be a candidate: %#v", got)
	}
	if err := os.MkdirAll(albumDir, 0755); err != nil {
		t.Fatal(err)
	}
	if got := artworkCandidateDirs(plan, dir); len(got) != 1 {
		t.Fatalf("directory without AlbumArt.jpg should remain a candidate: %#v", got)
	}
	if err := os.WriteFile(filepath.Join(albumDir, "AlbumArt.jpg"), []byte("art"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := artworkCandidateDirs(plan, dir); len(got) != 0 {
		t.Fatalf("existing AlbumArt.jpg should be skipped: %#v", got)
	}
}

func TestWriteArtworksCreatesDirectoryForMissingArtwork(t *testing.T) {
	dir := t.TempDir()
	plan := []Planned{{Relative: "Library/Artist/Album/song.m4a"}}
	artworkDirs := artworkCandidateDirs(plan, dir)
	transfers, err := makeArtworkTransferPlan(plan, dir, artworkDirs)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.write(false); err != nil {
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
	transfers, err := makeArtworkTransferPlan(plan, root, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.write(false); err != nil {
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
