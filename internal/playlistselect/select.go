// Package playlistselect implements the shared interactive playlist selector.
package playlistselect

import (
	"fmt"

	"music-bridge/internal/library"
	"music-bridge/internal/playlistfile"
	"music-bridge/internal/portable"
	"music-bridge/internal/tui"
)

const duplicateWarning = "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\r\n! 警告: 同名プレイリストが存在します                 !\r\n! このアプリは同名プレイリストに対応していません。   !\r\n! 名前で区別できないため、正しく同期できません。     !\r\n!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"

func DuplicateNames(playlists []library.Playlist) map[string]bool {
	seen := map[string]bool{}
	duplicates := map[string]bool{}
	for _, playlist := range playlists {
		name := playlistfile.SafeName(playlist.Name)
		key := portable.Key(name)
		if seen[key] {
			duplicates[name] = true
		}
		seen[key] = true
	}
	return duplicates
}

func Select(terminal *tui.Terminal, playlists []library.Playlist, existing map[string]bool) ([]library.Playlist, error) {
	if len(playlists) == 0 {
		return nil, fmt.Errorf("プレイリストがありません")
	}
	selected := map[int]bool{}
	for index, playlist := range playlists {
		if existing[playlistfile.SafeName(playlist.Name)] {
			selected[index] = true
		}
	}
	warning := ""
	if len(DuplicateNames(playlists)) > 0 {
		warning = duplicateWarning
	}
	indices, err := terminal.SelectMany(len(playlists), selected, warning, func(index int) string {
		playlist := playlists[index]
		count := playlist.TrackCount
		if count == 0 {
			count = len(playlist.Tracks)
		}
		return fmt.Sprintf("%s (%d曲)", playlist.Name, count)
	})
	if err != nil {
		return nil, err
	}
	result := make([]library.Playlist, 0, len(indices))
	for _, index := range indices {
		result = append(result, playlists[index])
	}
	return result, nil
}
