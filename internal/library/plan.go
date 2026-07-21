package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"music-bridge/internal/portable"
)

func MakePlan(playlists []Playlist) ([]Planned, []string, error) {
	seen := map[string]bool{}
	var plan []Planned
	var missing []string
	for _, playlist := range playlists {
		for _, track := range playlist.Tracks {
			if track.Location == "" {
				missing = append(missing, track.Name)
				continue
			}
			info, err := os.Stat(track.Location)
			if err != nil || !info.Mode().IsRegular() {
				missing = append(missing, track.Name)
				continue
			}
			if seen[track.Location] {
				continue
			}
			seen[track.Location] = true
			artist := track.AlbumArtist
			if artist == "" {
				artist = track.Artist
			}
			relative := filepath.Join(Directory, artist, track.Album, filepath.Base(track.Location))
			plan = append(plan, Planned{Track: track, Relative: relative, Size: info.Size()})
		}
	}
	return plan, missing, nil
}

func ValidatePlan(plan []Planned, playlists []Playlist) error {
	if len(plan) == 0 && CountTracks(playlists) > 0 {
		return fmt.Errorf("同期可能なローカル音源がありません。Musicライブラリの保存先ボリュームが接続されているか確認してください")
	}
	physicalPaths := map[string]string{}
	for _, item := range plan {
		physical := portable.AndroidVisible(filepath.ToSlash(item.Relative))
		key := strings.ToLower(physical)
		if previous, exists := physicalPaths[key]; exists && previous != item.Track.Location {
			return fmt.Errorf("Android上で同じパスになる音源があります: %s", physical)
		}
		physicalPaths[key] = item.Track.Location
	}
	return nil
}
