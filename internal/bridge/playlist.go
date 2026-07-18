package bridge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type playlistTargetFile struct {
	name string
	path string
	key  string
}

type playlistInventory struct {
	files []playlistTargetFile
	byKey map[string]playlistTargetFile
}

type playlistWrite struct {
	name string
	path string
	data []byte
}

type playlistSyncPlan struct {
	root     string
	selected map[string]bool
	writes   []playlistWrite
	stale    []playlistTargetFile
}

func scanPlaylistInventory(root string) (playlistInventory, error) {
	inventory := playlistInventory{byKey: map[string]playlistTargetFile{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return inventory, nil
		}
		return playlistInventory{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || isAppleDoublePath(entry.Name()) ||
			!strings.EqualFold(filepath.Ext(entry.Name()), ".m3u") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		file := playlistTargetFile{
			name: name,
			path: filepath.Join(root, entry.Name()),
			key:  portablePathKey(name),
		}
		inventory.files = append(inventory.files, file)
		if _, exists := inventory.byKey[file.key]; !exists {
			inventory.byKey[file.key] = file
		}
	}
	sort.Slice(inventory.files, func(i, j int) bool {
		return inventory.files[i].path < inventory.files[j].path
	})
	return inventory, nil
}

func (inventory playlistInventory) contains(name string) bool {
	_, exists := inventory.byKey[portablePathKey(safeName(name))]
	return exists
}

func (inventory playlistInventory) targetPath(root, name string) string {
	key := portablePathKey(safeName(name))
	if existing, exists := inventory.byKey[key]; exists {
		return portableMutationPath(existing.path)
	}
	return filepath.Join(root, androidVisiblePath(safeName(name)+".m3u"))
}

func makePlaylistSyncPlan(playlists []Playlist, tracks []Planned, root string) (playlistSyncPlan, error) {
	inventory, err := scanPlaylistInventory(root)
	if err != nil {
		return playlistSyncPlan{}, err
	}
	available := map[string]string{}
	for _, item := range tracks {
		available[item.Track.Location] = filepath.ToSlash(item.Relative)
	}
	result := playlistSyncPlan{
		root:     root,
		selected: map[string]bool{},
	}
	for _, playlist := range playlists {
		name := safeName(playlist.Name)
		result.selected[portablePathKey(name)] = true
		result.writes = append(result.writes, playlistWrite{
			name: name,
			path: inventory.targetPath(root, name),
			data: renderPlaylist(playlist, available),
		})
	}
	for _, file := range inventory.files {
		if !result.selected[file.key] {
			result.stale = append(result.stale, file)
		}
	}
	return result, nil
}

func (plan playlistSyncPlan) write(dry bool) error {
	for index, write := range plan.writes {
		fmt.Printf("プレイリスト生成中 [%d/%d] %s\n", index+1, len(plan.writes), write.name)
		if dry {
			continue
		}
		if current, err := os.ReadFile(write.path); err == nil && bytes.Equal(current, write.data) {
			continue
		}
		if err := os.WriteFile(write.path, write.data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (plan playlistSyncPlan) removeStale(dry bool) error {
	if dry {
		return nil
	}
	for _, file := range plan.stale {
		if err := removePortablePath(file.path); err != nil {
			return err
		}
	}
	current, err := scanPlaylistInventory(plan.root)
	if err != nil {
		return err
	}
	var remaining []string
	for _, file := range current.files {
		if !plan.selected[file.key] {
			remaining = append(remaining, file.path)
		}
	}
	if len(remaining) > 0 {
		logf("playlist M3U deletion failed; remaining paths: %v", remaining)
		return fmt.Errorf("選択されなかったプレイリストのM3Uを削除できませんでした（残り%d件）", len(remaining))
	}
	return nil
}

func renderPlaylist(playlist Playlist, available map[string]string) []byte {
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	for _, track := range playlist.Tracks {
		relative, ok := available[track.Location]
		if track.Location == "" || !ok {
			continue
		}
		builder.WriteString(androidVisiblePath(filepath.ToSlash(relative)))
		builder.WriteByte('\n')
	}
	return append([]byte{0xef, 0xbb, 0xbf}, []byte(builder.String())...)
}

func safeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")
	if s == "" {
		return "Unknown"
	}
	return s
}
