package bridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func artworkBytes(plan []Planned, root string) (int64, error) {
	seen := map[string]bool{}
	var total int64
	for _, item := range plan {
		if item.Track.Artwork == "" {
			continue
		}
		destinationDir := filepath.Join(root, filepath.Dir(item.Relative))
		if seen[destinationDir] {
			continue
		}
		sourceInfo, err := os.Stat(item.Track.Artwork)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if !sourceInfo.Mode().IsRegular() || sameFile(item.Track.Artwork, filepath.Join(destinationDir, "AlbumArt.jpg")) {
			if sourceInfo.Mode().IsRegular() {
				seen[destinationDir] = true
			}
			continue
		}
		seen[destinationDir] = true
		total += sourceInfo.Size()
	}
	return total, nil
}

func writeArtworks(plan []Planned, root string, dry bool, artworkDirs map[string]bool) error {
	started := time.Now()
	type artworkCopy struct {
		source      string
		destination string
		modTime     time.Time
	}
	plannedDirs := map[string]string{}
	sources := map[string]string{}
	for _, item := range plan {
		relativeDir := filepath.Dir(item.Relative)
		destinationDir := filepath.Join(root, relativeDir)
		plannedDirs[destinationDir] = relativeDir
		if item.Track.Artwork == "" || sources[destinationDir] != "" {
			continue
		}
		info, err := os.Stat(item.Track.Artwork)
		if err == nil && info.Mode().IsRegular() {
			sources[destinationDir] = item.Track.Artwork
		}
	}
	available := 0
	var copies []artworkCopy
	var emptyArtworkDirs []string
	for destinationDir, relativeDir := range plannedDirs {
		if !artworkDirs[relativeDir] {
			continue
		}
		destination := filepath.Join(destinationDir, "AlbumArt.jpg")
		source := sources[destinationDir]
		if source == "" {
			emptyArtworkDirs = append(emptyArtworkDirs, destinationDir)
			continue
		}
		sourceInfo, err := os.Stat(source)
		if err != nil || !sourceInfo.Mode().IsRegular() {
			continue
		}
		available++
		if sameFile(source, destination) {
			continue
		}
		copies = append(copies, artworkCopy{source, destination, sourceInfo.ModTime()})
	}
	sort.Slice(copies, func(i, j int) bool { return copies[i].destination < copies[j].destination })
	for _, destinationDir := range emptyArtworkDirs {
		if dry {
			continue
		}
		if err := os.MkdirAll(destinationDir, 0755); err != nil {
			return err
		}
	}
	if len(copies) > 0 && !dry {
		lastDraw := time.Time{}
		drawProgress := func(done int) {
			percent := float64(done) * 100 / float64(len(copies))
			fmt.Printf("\033[2K\rジャケ写を配置中 [%d/%d] %5.1f%%", done, len(copies), percent)
			lastDraw = time.Now()
		}
		drawProgress(0)
		for index, copy := range copies {
			if err := os.MkdirAll(filepath.Dir(copy.destination), 0755); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			if err := copyFile(copy.source, copy.destination, copy.modTime); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			done := index + 1
			if done == len(copies) || done%10 == 0 || time.Since(lastDraw) >= time.Second {
				drawProgress(done)
			}
		}
		fmt.Print("\033[2K\r\n")
	}
	fmt.Printf("ジャケ写: %d件取得 / %d件配置（処理時間: %s）\n", available, len(copies), time.Since(started).Round(time.Millisecond))
	return nil
}

func copyFile(source, destination string, modTime time.Time) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(destination, modTime, modTime)
}

func stagePlan(items []Planned) (string, error) {
	stage, err := os.MkdirTemp("", "music-bridge-stage-")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		path := filepath.Join(stage, item.Relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			os.RemoveAll(stage)
			return "", err
		}
		if err := os.Symlink(item.Track.Location, path); err != nil {
			os.RemoveAll(stage)
			return "", err
		}
	}
	return stage, nil
}
