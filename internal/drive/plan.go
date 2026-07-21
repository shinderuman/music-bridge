package drive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"music-bridge/internal/library"
	"music-bridge/internal/textfmt"
)

func countTracks(playlists []Playlist) int {
	return library.CountTracks(playlists)
}

func validatePlan(plan []Planned, playlists []Playlist) error {
	return library.ValidatePlan(plan, playlists)
}

func confirmDefaultYes(prompt string) bool {
	fmt.Print(prompt)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func makePlan(playlists []Playlist) ([]Planned, []string, error) {
	return library.MakePlan(playlists)
}

func totalBytes(plan []Planned) int64 {
	return library.TotalBytes(plan)
}
func humanBytes(n int64) string {
	return textfmt.HumanBytes(n)
}

func truncateRunes(value string, limit int) string {
	return textfmt.TruncateRunes(value, limit)
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

type audioTransferPlan struct {
	items []Planned
	bytes int64
}

func makeAudioTransferPlan(plan []Planned, root string) audioTransferPlan {
	result := audioTransferPlan{items: make([]Planned, 0, len(plan))}
	for _, item := range plan {
		if sameFile(item.Track.Location, filepath.Join(root, item.Relative)) {
			continue
		}
		result.items = append(result.items, item)
		result.bytes += item.Size
	}
	return result
}
