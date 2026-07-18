package bridge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

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

func TestValidatePlanRejectsAndroidVisiblePathCollision(t *testing.T) {
	tests := [][]Planned{
		{
			{Track: Track{Location: "/one"}, Relative: `Library/A:B/song.m4a`},
			{Track: Track{Location: "/two"}, Relative: "Library/A\uf022B/song.m4a"},
		},
		{
			{Track: Track{Location: "/one"}, Relative: `Library/Artist/song.m4a`},
			{Track: Track{Location: "/two"}, Relative: "Library/ARTIST/song.m4a"},
		},
	}
	for _, plan := range tests {
		if err := validatePlan(plan, []Playlist{{Tracks: []Track{{Location: "/one"}, {Location: "/two"}}}}); err == nil {
			t.Fatalf("Android-visible path collision was accepted: %#v", plan)
		}
	}
}
