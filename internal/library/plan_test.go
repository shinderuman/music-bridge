package library

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMakePlanBuildsPortableLibraryPaths(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.m4a")
	second := filepath.Join(root, "second.m4a")
	for _, source := range []string{first, second} {
		if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	playlists := []Playlist{{Tracks: []Track{
		{Name: "First", Artist: "Track Artist", AlbumArtist: "Album Artist", Album: "Album", Location: first},
		{Name: "Duplicate", Location: first},
		{Name: "Second", Artist: "Track Artist", Album: "Other", Location: second},
		{Name: "Missing", Location: filepath.Join(root, "missing.m4a")},
	}}}

	plan, missing, err := MakePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := missing, []string{"Missing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing=%#v, want %#v", got, want)
	}
	if len(plan) != 2 {
		t.Fatalf("plan=%#v", plan)
	}
	if got, want := plan[0].Relative, filepath.Join(Directory, "Album Artist", "Album", "first.m4a"); got != want {
		t.Fatalf("first relative=%q, want %q", got, want)
	}
	if got, want := plan[1].Relative, filepath.Join(Directory, "Track Artist", "Other", "second.m4a"); got != want {
		t.Fatalf("second relative=%q, want %q", got, want)
	}
	if CountTracks(playlists) != 4 || TotalBytes(plan) != 8 {
		t.Fatalf("counts: tracks=%d bytes=%d", CountTracks(playlists), TotalBytes(plan))
	}
}

func TestValidatePlanRejectsMissingSourcesAndPortablePathCollisions(t *testing.T) {
	playlists := []Playlist{{Tracks: []Track{{Location: "/missing"}}}}
	if err := ValidatePlan(nil, playlists); err == nil {
		t.Fatal("missing source plan was accepted")
	}
	plan := []Planned{
		{Track: Track{Location: "/one"}, Relative: "Library/A:B/song.m4a"},
		{Track: Track{Location: "/two"}, Relative: "Library/A\uf022B/song.m4a"},
	}
	if err := ValidatePlan(plan, playlists); err == nil {
		t.Fatal("portable path collision was accepted")
	}
}
