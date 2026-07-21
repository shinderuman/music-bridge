package drive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"music-bridge/internal/layout"
	"music-bridge/internal/targetlock"
)

const libraryManifestMarker = layout.LibraryManifestMarker
const legacyArtworkManifestMarker = ".music-bridge-artwork-indexed"
const albumArtworkFile = layout.AlbumArtwork

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
	return targetlock.Lock(root)
}

func staleAudio(plan []Planned, root string) ([]string, int64) {
	desired := map[string]bool{}
	desiredManaged := map[string]bool{}
	for _, item := range plan {
		desired[portablePathKey(filepath.ToSlash(item.Relative))] = true
		desiredManaged[portablePathKey(filepath.ToSlash(item.Relative))] = true
		desiredManaged[portablePathKey(filepath.ToSlash(
			filepath.Join(filepath.Dir(item.Relative), albumArtworkFile),
		))] = true
	}
	candidates := loadManagedPaths(root)
	candidates = append(candidates, localTransferPartials(root, candidates)...)
	if _, err := os.Stat(filepath.Join(root, libraryManifestMarker)); os.IsNotExist(err) {
		if legacy, scanErr := legacyLibraryFiles(root, false); scanErr == nil {
			candidates = append(candidates, legacy...)
		}
	}
	var stale []string
	var total int64
	for _, relative := range uniqueSortedPaths(candidates) {
		if !strings.HasPrefix(filepath.ToSlash(relative), libraryDir+"/") {
			continue
		}
		if isAppleDoublePath(relative) {
			continue
		}
		if filepath.Base(relative) == albumArtworkFile {
			continue
		}
		if destination, partial := transferPartialDestination(relative); partial {
			if desiredManaged[portablePathKey(filepath.ToSlash(destination))] {
				if _, err := os.Stat(filepath.Join(root, destination)); os.IsNotExist(err) {
					continue
				}
			}
		}
		if desired[portablePathKey(filepath.ToSlash(relative))] {
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

func staleManagementFiles(root string) []string {
	candidates := []string{
		legacyArtworkManifestMarker,
		manifest + ".tmp",
		pendingManifest + ".tmp",
		libraryManifestMarker + ".tmp",
	}
	var stale []string
	for _, relative := range candidates {
		if info, err := os.Stat(filepath.Join(root, relative)); err == nil && info.Mode().IsRegular() {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	return stale
}

func localTransferPartials(root string, managed []string) []string {
	var result []string
	for _, relative := range managed {
		if !strings.HasPrefix(filepath.ToSlash(relative), libraryDir+"/") {
			continue
		}
		for _, suffix := range []string{
			layout.PartialSuffix,
			layout.PartialSuffix + layout.PartialMetadataSuffix,
		} {
			partial := relative + suffix
			if info, err := os.Stat(filepath.Join(root, partial)); err == nil && info.Mode().IsRegular() {
				result = append(result, partial)
			}
		}
	}
	return uniqueSortedPaths(result)
}

func transferPartialDestination(relative string) (string, bool) {
	withoutMeta := strings.TrimSuffix(relative, layout.PartialMetadataSuffix)
	if !strings.HasSuffix(withoutMeta, layout.PartialSuffix) {
		return "", false
	}
	return strings.TrimSuffix(withoutMeta, layout.PartialSuffix), true
}

func staleArtwork(plan []Planned, root string) ([]string, int64, error) {
	desired := map[string]bool{}
	for _, item := range plan {
		relative := filepath.Join(filepath.Dir(item.Relative), albumArtworkFile)
		desired[portablePathKey(filepath.ToSlash(relative))] = true
	}
	var candidates []string
	if _, err := os.Stat(filepath.Join(root, libraryManifestMarker)); err == nil {
		for _, relative := range loadManagedPaths(root) {
			if filepath.Base(relative) == albumArtworkFile {
				candidates = append(candidates, relative)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, 0, err
	} else {
		var err error
		candidates, err = legacyLibraryFiles(root, true)
		if err != nil {
			return nil, 0, err
		}
	}
	var stale []string
	var total int64
	for _, relative := range candidates {
		if desired[portablePathKey(filepath.ToSlash(relative))] {
			continue
		}
		info, err := os.Stat(filepath.Join(root, relative))
		if err == nil && info.Mode().IsRegular() {
			stale = append(stale, relative)
			total += info.Size()
		}
	}
	sort.Strings(stale)
	return stale, total, nil
}

func legacyLibraryFiles(root string, artwork bool) ([]string, error) {
	libraryRoot := filepath.Join(root, libraryDir)
	var result []string
	err := filepath.WalkDir(libraryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || (entry.Name() == albumArtworkFile) != artwork {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result = append(result, relative)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(result)
	return result, nil
}

func loadManagedPaths(root string) []string {
	result := make([]string, 0)
	for _, relative := range loadManifest(root) {
		result = append(result, localManagedPath(relative))
	}
	data, err := os.ReadFile(filepath.Join(root, pendingManifest))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if relative := strings.TrimSpace(line); relative != "" {
				result = append(result, localManagedPath(relative))
			}
		}
	}
	return uniqueSortedPaths(result)
}

func localManagedPath(relative string) string {
	logical := logicalPathFromAndroid(filepath.ToSlash(relative))
	return filepath.FromSlash(logical)
}

func uniqueSortedPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" && !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result
}

func savePendingManifest(root string, plan []Planned) error {
	paths := loadManagedPaths(root)
	for _, item := range plan {
		paths = append(paths, item.Relative)
		if item.Track.Artwork != "" {
			paths = append(paths, filepath.Join(filepath.Dir(item.Relative), albumArtworkFile))
		}
	}
	for index := range paths {
		paths[index] = androidVisiblePath(filepath.ToSlash(paths[index]))
	}
	paths = uniqueSortedPaths(paths)
	var data strings.Builder
	for _, relative := range paths {
		data.WriteString(relative)
		data.WriteByte('\n')
	}
	return writeFileAtomically(filepath.Join(root, pendingManifest), []byte(data.String()), 0644)
}

func saveManifest(root string, plan []Planned) error {
	paths := make([]string, 0, len(plan)*2)
	seen := map[string]bool{}
	for _, item := range plan {
		relative := androidVisiblePath(filepath.ToSlash(item.Relative))
		if !seen[relative] {
			paths = append(paths, relative)
			seen[relative] = true
		}
		artwork := androidVisiblePath(filepath.ToSlash(
			filepath.Join(filepath.Dir(item.Relative), albumArtworkFile),
		))
		if seen[artwork] {
			continue
		}
		logicalArtwork := filepath.Join(filepath.Dir(item.Relative), albumArtworkFile)
		if info, err := os.Stat(filepath.Join(root, logicalArtwork)); err == nil && info.Mode().IsRegular() {
			paths = append(paths, artwork)
			seen[artwork] = true
		}
	}
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || isAppleDoublePath(entry.Name()) || filepath.Ext(entry.Name()) != ".m3u" {
				continue
			}
			relative := androidVisiblePath(entry.Name())
			if seen[relative] {
				continue
			}
			paths = append(paths, relative)
			seen[relative] = true
		}
	}
	sort.Strings(paths)
	if err := saveManifestPaths(root, paths); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, libraryManifestMarker)); os.IsNotExist(err) {
		if err := writeFileAtomically(
			filepath.Join(root, libraryManifestMarker),
			[]byte("Library paths are indexed in the manifest.\n"),
			0644,
		); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, pendingManifest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func saveManifestPaths(root string, paths []string) error {
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	destination := filepath.Join(root, manifest)
	data = append(data, '\n')
	if current, err := os.ReadFile(destination); err == nil && bytes.Equal(current, data) {
		return nil
	}
	return writeFileAtomically(destination, data, 0644)
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
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
		slashed := filepath.ToSlash(path)
		if !strings.HasPrefix(slashed, libraryDir+"/") &&
			!(filepath.Ext(path) == ".m3u" && !strings.Contains(slashed, "/")) {
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
