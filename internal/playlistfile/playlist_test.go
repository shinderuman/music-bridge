package playlistfile

import (
	"testing"

	"music-bridge/internal/library"
)

func TestSafeNameAndRender(t *testing.T) {
	if got := SafeName(" /A\\B/ "); got != "AB" {
		t.Fatalf("SafeName = %q", got)
	}
	playlist := library.Playlist{Tracks: []library.Track{
		{Location: "/music/one.m4a"},
		{Location: "/music/missing.m4a"},
	}}
	got := string(Render(playlist, map[string]string{"/music/one.m4a": "Library/A:B/one.m4a"}))
	want := "\xef\xbb\xbf#EXTM3U\nLibrary/A\uf022B/one.m4a\n"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}
