package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func countTracks(playlists []Playlist) int {
	total := 0
	for _, p := range playlists {
		total += len(p.Tracks)
	}
	return total
}

func validatePlan(plan []Planned, playlists []Playlist) error {
	if len(plan) == 0 && countTracks(playlists) > 0 {
		return fmt.Errorf("同期可能なローカル音源がありません。Musicライブラリの保存先ボリュームが接続されているか確認してください")
	}
	return nil
}

func confirmDefaultYes(prompt string) bool {
	fmt.Print(prompt)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func makePlan(playlists []Playlist) ([]Planned, []string, error) {
	seen := map[string]bool{}
	var plan []Planned
	var missing []string
	for _, p := range playlists {
		for _, t := range p.Tracks {
			if t.Location == "" {
				missing = append(missing, t.Name)
				continue
			}
			info, err := os.Stat(t.Location)
			if err != nil || !info.Mode().IsRegular() {
				missing = append(missing, t.Name)
				continue
			}
			if seen[t.Location] {
				continue
			}
			seen[t.Location] = true
			artist := t.AlbumArtist
			if artist == "" {
				artist = t.Artist
			}
			rel := filepath.Join(libraryDir, artist, t.Album, filepath.Base(t.Location))
			plan = append(plan, Planned{t, rel, info.Size()})
		}
	}
	return plan, missing, nil
}

func totalBytes(plan []Planned) int64 {
	var total int64
	for _, p := range plan {
		total += p.Size
	}
	return total
}
func humanBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func freeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func sameFile(source, destination string) bool {
	ai, err := os.Stat(source)
	if err != nil {
		return false
	}
	bi, err := os.Stat(destination)
	if err != nil {
		return false
	}
	modTimeDelta := ai.ModTime().Sub(bi.ModTime())
	if modTimeDelta < 0 {
		modTimeDelta = -modTimeDelta
	}
	// exFATでは更新日時の精度が低く、macOS側と数ms〜数十msずれる。
	return ai.Mode().IsRegular() && bi.Mode().IsRegular() &&
		ai.Size() == bi.Size() && modTimeDelta <= 2*time.Second
}

func existingBytes(plan []Planned, root string) (int64, error) {
	var total int64
	for _, p := range plan {
		destination := filepath.Join(root, p.Relative)
		if !sameFile(p.Track.Location, destination) {
			total += p.Size
		}
	}
	return total, nil
}
