package bridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type artworkCopy struct {
	source      string
	destination string
	modTime     time.Time
}

type artworkTransferPlan struct {
	available int
	bytes     int64
	copies    []artworkCopy
	emptyDirs []string
}

func makeArtworkTransferPlan(tracks []Planned, root string, candidateDirs map[string]bool) (artworkTransferPlan, error) {
	result := artworkTransferPlan{}
	plannedDirs := map[string]bool{}
	sources := map[string]string{}
	for _, item := range tracks {
		relativeDir := filepath.Dir(item.Relative)
		if !candidateDirs[relativeDir] {
			continue
		}
		destinationDir := filepath.Join(root, relativeDir)
		plannedDirs[destinationDir] = true
		if item.Track.Artwork == "" || sources[destinationDir] != "" {
			continue
		}
		info, err := os.Stat(item.Track.Artwork)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return artworkTransferPlan{}, err
		}
		if info.Mode().IsRegular() {
			sources[destinationDir] = item.Track.Artwork
		}
	}
	for destinationDir := range plannedDirs {
		destination := filepath.Join(destinationDir, albumArtworkFile)
		if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() {
			// 一度配置済みのアルバムは更新しない。容量表示と配置処理の両方で
			// 同じ判定を使うため、既存画像を再転送分として数えない。
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return artworkTransferPlan{}, err
		}
		source := sources[destinationDir]
		if source == "" {
			result.emptyDirs = append(result.emptyDirs, destinationDir)
			continue
		}
		sourceInfo, err := os.Stat(source)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return artworkTransferPlan{}, err
		}
		if !sourceInfo.Mode().IsRegular() {
			continue
		}
		result.available++
		result.bytes += sourceInfo.Size()
		result.copies = append(result.copies, artworkCopy{
			source:      source,
			destination: destination,
			modTime:     sourceInfo.ModTime(),
		})
	}
	sort.Slice(result.copies, func(i, j int) bool {
		return result.copies[i].destination < result.copies[j].destination
	})
	sort.Strings(result.emptyDirs)
	return result, nil
}

func (plan artworkTransferPlan) write(dry bool) error {
	started := time.Now()
	for _, destinationDir := range plan.emptyDirs {
		if dry {
			continue
		}
		if err := os.MkdirAll(destinationDir, 0755); err != nil {
			return err
		}
	}
	if len(plan.copies) > 0 && !dry {
		lastDraw := time.Time{}
		drawProgress := func(done int) {
			percent := float64(done) * 100 / float64(len(plan.copies))
			fmt.Printf("\033[2K\rジャケ写を配置中 [%d/%d] %5.1f%%", done, len(plan.copies), percent)
			lastDraw = time.Now()
		}
		drawProgress(0)
		for index, copy := range plan.copies {
			if err := os.MkdirAll(filepath.Dir(copy.destination), 0755); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			if err := copyFile(copy.source, copy.destination, copy.modTime); err != nil {
				fmt.Print("\033[2K\r")
				return err
			}
			done := index + 1
			if done == len(plan.copies) || done%10 == 0 || time.Since(lastDraw) >= time.Second {
				drawProgress(done)
			}
		}
		fmt.Print("\033[2K\r\n")
	}
	fmt.Printf("ジャケ写: %d件取得 / %d件配置（処理時間: %s）\n",
		plan.available, len(plan.copies), time.Since(started).Round(time.Millisecond))
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
