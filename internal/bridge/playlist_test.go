package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeName(t *testing.T) {
	if got := safeName(" A/B "); got != "AB" {
		t.Fatalf("safeName = %q", got)
	}
	if got := safeName(" "); got != "Unknown" {
		t.Fatalf("empty safeName = %q", got)
	}
}

func TestRenderPlaylistUsesOnlyAvailableTracks(t *testing.T) {
	playlist := Playlist{Tracks: []Track{
		{Location: "/music/one.m4a"},
		{Location: "/music/missing.m4a"},
	}}
	data := renderPlaylist(playlist, map[string]string{"/music/one.m4a": "Library/A/one.m4a"})
	if got, want := string(data), "\xef\xbb\xbf#EXTM3U\nLibrary/A/one.m4a\n"; got != want {
		t.Fatalf("playlist=%q, want %q", got, want)
	}
}

func TestRenderPlaylistUsesAndroidVisiblePath(t *testing.T) {
	playlist := Playlist{Tracks: []Track{{Location: "/music/song.m4a"}}}
	available := map[string]string{
		"/music/song.m4a": `Library/Artist:Name/Album "Best"/song?.m4a`,
	}
	data := renderPlaylist(playlist, available)
	want := "\xef\xbb\xbf#EXTM3U\nLibrary/Artist\uf022Name/Album \uf020Best\uf020/song\uf025.m4a\n"
	if got := string(data); got != want {
		t.Fatalf("playlist=%q, want %q", got, want)
	}
}

func TestPlaylistFilenameAndExistingSelectionUsePortableLogicalName(t *testing.T) {
	root := t.TempDir()
	playlist := Playlist{Name: "Question?Mark"}
	syncPlan, err := makePlaylistSyncPlan([]Playlist{playlist}, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncPlan.write(false); err != nil {
		t.Fatal(err)
	}
	encoded := androidVisiblePath("Question?Mark.m3u")
	if _, err := os.Stat(filepath.Join(root, encoded)); err != nil {
		t.Fatalf("portable playlist file missing: %v", err)
	}
	inventory, err := scanPlaylistInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !inventory.contains("Question?Mark") || len(inventory.files) != 1 {
		t.Fatalf("existing playlist inventory=%#v", inventory)
	}
	syncPlan, err = makePlaylistSyncPlan([]Playlist{{Name: "question?mark"}}, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPlan.stale) != 0 {
		t.Fatalf("selected portable playlist treated as stale: %#v", syncPlan.stale)
	}
}

func TestPlaylistInventoryNormalizesUnicodeAndIgnoresAppleDouble(t *testing.T) {
	root := t.TempDir()
	decomposed := "あとか\u3099たり.m3u"
	for _, name := range []string{decomposed, "._" + decomposed} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("#EXTM3U\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := scanPlaylistInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !inventory.contains("あとがたり") {
		t.Fatalf("NFD playlist did not match NFC name: %#v", inventory)
	}
	if len(inventory.files) != 1 {
		t.Fatalf("AppleDouble playlist was included: %#v", inventory)
	}
	syncPlan, err := makePlaylistSyncPlan([]Playlist{{Name: "あとがたり"}}, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPlan.stale) != 0 {
		t.Fatalf("NFD selected playlist treated as stale: %#v", syncPlan.stale)
	}
}

func TestPlaylistSyncPlanRemovesOnlyStaleFilesAndVerifiesResult(t *testing.T) {
	root := t.TempDir()
	selected := []Playlist{{Name: "Keep"}}
	for _, name := range []string{"Keep.m3u", "Remove.m3u"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("#EXTM3U\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	syncPlan, err := makePlaylistSyncPlan(selected, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPlan.stale) != 1 {
		t.Fatalf("stale playlists=%#v, want one", syncPlan.stale)
	}
	if err := syncPlan.removeStale(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Keep.m3u")); err != nil {
		t.Fatalf("selected playlist was removed: %v", err)
	}
	inventory, err := scanPlaylistInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.files) != 1 || !inventory.contains("Keep") {
		t.Fatalf("playlist inventory after deletion=%#v", inventory)
	}
}

func TestPlaylistSyncPlanResolvesExistingUnicodeNameForMutation(t *testing.T) {
	root := t.TempDir()
	decomposed := "あとか\u3099たり.m3u"
	path := filepath.Join(root, decomposed)
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	syncPlan, err := makePlaylistSyncPlan([]Playlist{{Name: "あとがたり"}}, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	want := portableMutationPath(path)
	if len(syncPlan.writes) != 1 || syncPlan.writes[0].path != want {
		t.Fatalf("write path=%#v, want resolved path %q", syncPlan.writes, want)
	}
}

func TestPlaylistSyncOnExFAT(t *testing.T) {
	root := os.Getenv("MUSIC_BRIDGE_EXFAT_TEST_ROOT")
	if root == "" {
		t.Skip("MUSIC_BRIDGE_EXFAT_TEST_ROOT is not set")
	}
	testRoot, err := os.MkdirTemp(root, "music-bridge-playlist-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testRoot)
	decomposed := "ミューシ\u3099ックヒ\u3099テ\u3099オ.m3u"
	existingPath := filepath.Join(testRoot, decomposed)
	if err := os.WriteFile(existingPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	selected := []Playlist{{Name: "ミュージックビデオ"}}
	syncPlan, err := makePlaylistSyncPlan(selected, nil, testRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPlan.stale) != 0 {
		t.Fatalf("selected NFD playlist was stale: %#v", syncPlan.stale)
	}
	if err := syncPlan.write(false); err != nil {
		t.Fatal(err)
	}
	inventory, err := scanPlaylistInventory(testRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.files) != 1 {
		t.Fatalf("playlist write created duplicate exFAT entries: %#v", inventory.files)
	}
	data, err := os.ReadFile(portableMutationPath(inventory.files[0].path))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "\xef\xbb\xbf#EXTM3U\n"; got != want {
		t.Fatalf("updated exFAT playlist=%q, want %q", got, want)
	}
	syncPlan, err = makePlaylistSyncPlan(nil, nil, testRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncPlan.removeStale(false); err != nil {
		t.Fatal(err)
	}
	inventory, err = scanPlaylistInventory(testRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.files) != 0 {
		t.Fatalf("playlist remains on exFAT: %#v", inventory.files)
	}
}

func TestDuplicatePlaylistNamesAreCaseInsensitive(t *testing.T) {
	got := duplicatePlaylistNames([]Playlist{{Name: "GAME"}, {Name: "game"}})
	if !got["game"] || len(got) != 1 {
		t.Fatalf("duplicates=%#v", got)
	}
}
