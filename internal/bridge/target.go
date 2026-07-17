package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const syncLock = ".music-bridge-sync.lock"

func loadManifest(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		return nil
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return nil
	}
	return paths
}

func lockTarget(root string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(root, syncLock), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("この同期先は別のmusic-bridgeが同期中です: %s", root)
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func staleAudio(plan []Planned, root string) ([]string, int64) {
	desired := map[string]bool{}
	for _, item := range plan {
		desired[item.Relative] = true
	}
	var stale []string
	var total int64
	for _, relative := range loadManifest(root) {
		if desired[relative] {
			continue
		}
		info, err := os.Stat(filepath.Join(root, relative))
		if err == nil && info.Mode().IsRegular() {
			stale = append(stale, relative)
			total += info.Size()
		}
	}
	sort.Strings(stale)
	return stale, total
}

func saveManifest(root string, plan []Planned) error {
	paths := make([]string, 0, len(plan))
	for _, item := range plan {
		paths = append(paths, item.Relative)
	}
	sort.Strings(paths)
	return saveManifestPaths(root, paths)
}

func saveManifestPaths(root string, paths []string) error {
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, manifest), append(data, '\n'), 0644)
}

func migrateLegacyLayout(root string) error {
	libraryRoot := filepath.Join(root, libraryDir)
	if info, err := os.Stat(libraryRoot); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("ライブラリディレクトリがディレクトリではありません: %s", libraryRoot)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var legacyDirs []string
	type playlistFile struct {
		path string
		data []byte
	}
	var playlists []playlistFile
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			legacyDirs = append(legacyDirs, entry.Name())
			continue
		}
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".m3u" {
			path := filepath.Join(root, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			playlists = append(playlists, playlistFile{path, prefixLibraryInM3U(data)})
		}
	}
	if len(legacyDirs) == 0 {
		return nil
	}

	manifestPaths := loadManifest(root)
	for i, path := range manifestPaths {
		if !strings.HasPrefix(filepath.ToSlash(path), libraryDir+"/") {
			manifestPaths[i] = filepath.Join(libraryDir, path)
		}
	}

	fmt.Printf("既存ライブラリを %s/ へ移行中...\n", libraryDir)
	if err := os.Mkdir(libraryRoot, 0755); err != nil {
		return err
	}
	for _, name := range legacyDirs {
		if err := os.Rename(filepath.Join(root, name), filepath.Join(libraryRoot, name)); err != nil {
			return err
		}
	}
	for _, playlist := range playlists {
		if err := os.WriteFile(playlist.path, playlist.data, 0644); err != nil {
			return err
		}
	}
	if len(manifestPaths) > 0 {
		sort.Strings(manifestPaths)
		if err := saveManifestPaths(root, manifestPaths); err != nil {
			return err
		}
	}
	fmt.Printf("ライブラリ移行完了: %dディレクトリ\n", len(legacyDirs))
	return nil
}

func prefixLibraryInM3U(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		ending := ""
		if strings.HasSuffix(line, "\r") {
			line = strings.TrimSuffix(line, "\r")
			ending = "\r"
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "\ufeff#") || strings.HasPrefix(line, libraryDir+"/") {
			continue
		}
		lines[i] = filepath.ToSlash(filepath.Join(libraryDir, line)) + ending
	}
	return []byte(strings.Join(lines, "\n"))
}

func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
	}
	return nil
}

func fitPlan(plan []Planned, root string, free int64) []Planned {
	result := []Planned{}
	for _, p := range plan {
		destination := filepath.Join(root, p.Relative)
		if sameFile(p.Track.Location, destination) {
			result = append(result, p)
			continue
		}
		if p.Size <= free {
			result = append(result, p)
			free -= p.Size
		}
	}
	return result
}
