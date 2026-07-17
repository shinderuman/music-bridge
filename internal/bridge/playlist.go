package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func writePlaylists(playlists []Playlist, plan []Planned, root string, dry bool) error {
	available := map[string]string{}
	for _, item := range plan {
		available[item.Track.Location] = filepath.ToSlash(item.Relative)
	}
	for i, p := range playlists {
		fmt.Printf("プレイリスト生成中 [%d/%d] %s\n", i+1, len(playlists), p.Name)
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		for _, t := range p.Tracks {
			relative, ok := available[t.Location]
			if t.Location == "" || !ok {
				continue
			}
			b.WriteString(relative)
			b.WriteByte('\n')
		}
		if !dry {
			path := filepath.Join(root, safeName(p.Name)+".m3u")
			if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(b.String())...), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func stalePlaylists(all, selected []Playlist, root string) []string {
	wanted := map[string]bool{}
	for _, p := range selected {
		wanted[safeName(p.Name)] = true
	}
	known := map[string]bool{}
	for _, p := range all {
		known[safeName(p.Name)] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".m3u" {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".m3u")
		if known[stem] && !wanted[stem] {
			result = append(result, filepath.Join(root, e.Name()))
		}
	}
	sort.Strings(result)
	return result
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
