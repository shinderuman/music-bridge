package bridge

import (
	"path/filepath"
	"reflect"
	"testing"
)

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
