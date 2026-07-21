// Package playlistfile renders Android-compatible M3U playlists.
package playlistfile

import (
	"path/filepath"
	"strings"

	"music-bridge/internal/library"
	"music-bridge/internal/portable"
)

func SafeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "")
	value = strings.ReplaceAll(value, "\\", "")
	if value == "" {
		return "Unknown"
	}
	return value
}

func Render(playlist library.Playlist, available map[string]string) []byte {
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	for _, track := range playlist.Tracks {
		relative, ok := available[track.Location]
		if track.Location == "" || !ok {
			continue
		}
		builder.WriteString(portable.AndroidVisible(filepath.ToSlash(relative)))
		builder.WriteByte('\n')
	}
	return append([]byte{0xef, 0xbb, 0xbf}, []byte(builder.String())...)
}
