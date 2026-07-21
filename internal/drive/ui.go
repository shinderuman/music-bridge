package drive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"music-bridge/internal/playlistselect"
	"music-bridge/internal/tui"
)

var terminalUI = tui.System()

func chooseMany(playlists []Playlist, root string) ([]Playlist, error) {
	existing := map[string]bool{}
	if inventory, err := scanPlaylistInventory(root); err == nil {
		for _, playlist := range playlists {
			if inventory.contains(playlist.Name) {
				existing[safeName(playlist.Name)] = true
			}
		}
	}
	return chooseManyWithExisting(playlists, existing)
}

func chooseManyWithExisting(playlists []Playlist, existing map[string]bool) ([]Playlist, error) {
	return playlistselect.Select(terminalUI, playlists, existing)
}

func duplicatePlaylistNames(playlists []Playlist) map[string]bool {
	return playlistselect.DuplicateNames(playlists)
}

func chooseTarget() (string, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return "", err
	}
	volumes := []string{}
	for _, e := range entries {
		if e.IsDir() {
			volumes = append(volumes, filepath.Join("/Volumes", e.Name()))
		}
	}
	sort.Strings(volumes)
	if len(volumes) == 0 {
		return "", fmt.Errorf("/Volumesに同期先がありません")
	}
	index, err := interactiveOne(volumes, "同期先を選択してください", func(i int) string { return volumes[i] })
	if err != nil {
		return "", err
	}
	return volumes[index], nil
}

func interactiveOne(items []string, title string, label func(int) string) (int, error) {
	return terminalUI.SelectOne(len(items), title, label)
}
