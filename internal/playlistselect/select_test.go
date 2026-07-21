package playlistselect

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"music-bridge/internal/library"
	"music-bridge/internal/tui"
)

func TestDuplicateNamesAndExistingSelection(t *testing.T) {
	playlists := []library.Playlist{{Name: "GAME", TrackCount: 1}, {Name: "game", TrackCount: 2}}
	if got := DuplicateNames(playlists); !got["game"] || len(got) != 1 {
		t.Fatalf("duplicates = %#v", got)
	}
	var output bytes.Buffer
	terminal := tui.New(strings.NewReader("\r"), &output)
	got, err := Select(terminal, playlists, map[string]bool{"GAME": true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, playlists[:1]) {
		t.Fatalf("selected = %#v", got)
	}
	if !strings.Contains(output.String(), "同名プレイリスト") {
		t.Fatalf("warning missing from %q", output.String())
	}
}
