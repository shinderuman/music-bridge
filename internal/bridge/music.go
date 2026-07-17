package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Track struct {
	Name        string `json:"name"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"album_artist"`
	Album       string `json:"album"`
	Location    string `json:"location"`
	Artwork     string `json:"artwork,omitempty"`
}

type Playlist struct {
	Name       string  `json:"name"`
	TrackCount int     `json:"trackCount,omitempty"`
	Tracks     []Track `json:"tracks,omitempty"`
}

const playlistCacheVersion = 2

type playlistCache struct {
	Version   int                       `json:"version"`
	Playlists map[string]cachedPlaylist `json:"playlists"`
}

type cachedPlaylist struct {
	Playlist    Playlist `json:"playlist"`
	Fingerprint string   `json:"fingerprint"`
}

type playlistFingerprint struct {
	Name       string   `json:"name"`
	TrackCount int      `json:"trackCount"`
	TrackIDs   []string `json:"trackIDs"`
}

func sourceArgs(source string, summary bool, fingerprint bool, names []string, progressPrefix string) []string {
	if source == "" {
		source = bundledScript("export_music_library.js")
	}
	args := []string{"-l", "JavaScript", source}
	if summary {
		args = append(args, "--summary")
	}
	if fingerprint {
		args = append(args, "--fingerprint")
	}
	if progressPrefix != "" {
		args = append(args, "--progress-prefix", progressPrefix)
	}
	for _, name := range names {
		args = append(args, "--playlist", name)
	}
	return args
}

func bundledScript(name string) string {
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		candidate := filepath.Join(filepath.Dir(executable), "scripts", name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	// go run とリポジトリ直下でビルドした開発用バイナリでは、現在の
	// 作業ディレクトリにある scripts/ を使う。
	return filepath.Join("scripts", name)
}

func loadPlaylists(source string, summary bool, names []string) ([]Playlist, error) {
	return loadPlaylistsWithProgress(source, summary, names, "")
}

func loadPlaylistsWithProgress(source string, summary bool, names []string, progressPrefix string) ([]Playlist, error) {
	if source != "" {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		var playlists []Playlist
		if err := json.Unmarshal(data, &playlists); err != nil {
			return nil, err
		}
		return filterPlaylists(playlists, names), nil
	}
	out, err := runMusicScript(sourceArgs(source, summary, false, names, progressPrefix))
	if err != nil {
		return nil, err
	}
	var playlists []Playlist
	if err := json.Unmarshal(out, &playlists); err != nil {
		return nil, fmt.Errorf("取得結果がJSONではありません: %w", err)
	}
	return playlists, nil
}

func runMusicScript(args []string) ([]byte, error) {
	cmd := exec.Command("osascript", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Music.appを起動できませんでした: %w", err)
	}
	var waiting sync.WaitGroup
	stopWaiting := make(chan struct{})
	waiting.Add(1)
	go func() {
		defer waiting.Done()
		started := time.Now()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\033[2K\r  Music.appの応答を待機中... %d秒", int(time.Since(started).Seconds()))
			case <-stopWaiting:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(stopWaiting)
	waiting.Wait()
	fmt.Fprint(os.Stderr, "\033[2K\r")
	if err != nil {
		return nil, fmt.Errorf("Music.appから取得できませんでした: %w", err)
	}
	return out.Bytes(), nil
}

func playlistCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Music Bridge", "library-cache.json"), nil
}

func loadSyncPlaylists(source string, selected []Playlist, refresh bool) ([]Playlist, error) {
	names := make([]string, len(selected))
	for i, playlist := range selected {
		names[i] = playlist.Name
	}
	if source != "" {
		fmt.Println("選択したプレイリストの曲情報を取得中...")
		return loadPlaylists(source, false, names)
	}
	path, err := playlistCachePath()
	if err != nil {
		return nil, err
	}
	cache := loadPlaylistCache(path)
	if cache.Playlists == nil {
		cache.Playlists = map[string]cachedPlaylist{}
	}
	fingerprints, err := loadPlaylistFingerprints(names)
	if err != nil {
		return nil, err
	}
	fingerprintByName := map[string]string{}
	for _, fingerprint := range fingerprints {
		fingerprintByName[fingerprint.Name] = fingerprintHash(fingerprint.TrackIDs)
	}
	for _, name := range names {
		if _, ok := fingerprintByName[name]; !ok {
			return nil, fmt.Errorf("Music.appから曲ID一覧を取得できませんでした: %s", name)
		}
	}
	var fetchNames []string
	for _, name := range names {
		cached, ok := cache.Playlists[name]
		if refresh || !ok || cached.Fingerprint != fingerprintByName[name] {
			fetchNames = append(fetchNames, name)
		}
	}
	if len(fetchNames) > 0 {
		if refresh {
			fmt.Printf("選択したプレイリストの曲情報をMusic.appから更新中...（%d件）\n", len(fetchNames))
		} else {
			fmt.Printf("変更されたプレイリストの曲情報をMusic.appから取得中...（%d件）\n", len(fetchNames))
		}
		for index, name := range fetchNames {
			progressPrefix := fmt.Sprintf("曲情報を取得中 [%d/%d] %s", index+1, len(fetchNames), name)
			fetched, err := loadPlaylistsWithProgress("", false, []string{name}, progressPrefix)
			if err != nil {
				return nil, err
			}
			if len(fetched) != 1 {
				return nil, fmt.Errorf("Music.appからプレイリストを取得できませんでした: %s", name)
			}
			cache.Playlists[name] = cachedPlaylist{Playlist: fetched[0], Fingerprint: fingerprintByName[name]}
			cache.Version = playlistCacheVersion
			if err := savePlaylistCache(path, cache); err != nil {
				return nil, err
			}
		}
	} else {
		fmt.Printf("曲情報キャッシュを使用します（%d件、Music.appの曲ID一覧と照合済み）\n", len(names))
	}
	playlists := make([]Playlist, 0, len(names))
	for _, name := range names {
		cached, ok := cache.Playlists[name]
		if !ok {
			return nil, fmt.Errorf("曲情報キャッシュにプレイリストがありません: %s", name)
		}
		playlists = append(playlists, cached.Playlist)
	}
	return playlists, nil
}

func loadPlaylistFingerprints(names []string) ([]playlistFingerprint, error) {
	args := sourceArgs("", false, true, names, "")
	out, err := runMusicScript(args)
	if err != nil {
		return nil, err
	}
	var fingerprints []playlistFingerprint
	if err := json.Unmarshal(out, &fingerprints); err != nil {
		return nil, fmt.Errorf("曲ID一覧の取得結果がJSONではありません: %w", err)
	}
	return fingerprints, nil
}

func fingerprintHash(trackIDs []string) string {
	hash := sha256.New()
	for _, id := range trackIDs {
		hash.Write([]byte(id))
		hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func loadPlaylistCache(path string) playlistCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return playlistCache{}
	}
	var cache playlistCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != playlistCacheVersion {
		return playlistCache{}
	}
	return cache
}

func savePlaylistCache(path string, cache playlistCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".library-cache-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

type artworkRequest struct {
	playlistIndex int
	trackIndex    int
}

func artworkRequests(playlists []Playlist, plan []Planned, artworkDirs map[string]bool) []artworkRequest {
	relativeByLocation := make(map[string]string, len(plan))
	for _, item := range plan {
		relativeByLocation[item.Track.Location] = item.Relative
	}
	var requests []artworkRequest
	for playlistIndex, playlist := range playlists {
		for trackIndex, track := range playlist.Tracks {
			relative, ok := relativeByLocation[track.Location]
			if !ok {
				continue
			}
			if !artworkDirs[filepath.Dir(relative)] {
				continue
			}
			// アルバム内の曲ごとにアートワークの有無が異なり得るため、
			// 未配置アルバムでは全曲を候補にする。既存アルバムは丸ごと省略する。
			requests = append(requests, artworkRequest{playlistIndex + 1, trackIndex + 1})
		}
	}
	return requests
}

func artworkCandidateDirs(plan []Planned, root string) map[string]bool {
	candidates := map[string]bool{}
	checked := map[string]bool{}
	for _, item := range plan {
		relativeDir := filepath.Dir(item.Relative)
		if checked[relativeDir] {
			continue
		}
		checked[relativeDir] = true
		if _, err := os.Stat(filepath.Join(root, relativeDir)); err == nil {
			continue
		}
		candidates[relativeDir] = true
	}
	return candidates
}

func exportArtworks(playlists []Playlist, artworkDir string, requests []artworkRequest) error {
	if len(requests) == 0 {
		return nil
	}
	requestPath := filepath.Join(artworkDir, ".artwork-requests")
	var requestData strings.Builder
	for _, request := range requests {
		fmt.Fprintf(&requestData, "%d:%d\n", request.playlistIndex, request.trackIndex)
	}
	if err := os.WriteFile(requestPath, []byte(requestData.String()), 0600); err != nil {
		return err
	}
	total := len(requests)
	progressPath := filepath.Join(artworkDir, ".artwork-progress")
	args := []string{bundledScript("export_music_artwork.applescript"), artworkDir, progressPath, requestPath, strconv.Itoa(total)}
	for _, playlist := range playlists {
		args = append(args, playlist.Name)
	}
	cmd := exec.Command("osascript", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Music.appからジャケ写を取得できませんでした: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\033[2K\rジャケ写を取得中... 0/%d曲", total)
	stopProgress := make(chan struct{})
	var waiting sync.WaitGroup
	waiting.Add(1)
	go func() {
		defer waiting.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		last := ""
		for {
			select {
			case <-ticker.C:
				data, err := os.ReadFile(progressPath)
				if err != nil || len(data) == 0 {
					continue
				}
				parts := strings.Split(strings.TrimSpace(string(data)), "/")
				if len(parts) != 2 {
					continue
				}
				line := fmt.Sprintf("ジャケ写を取得中... %s/%s曲", parts[0], parts[1])
				if line != last {
					fmt.Fprintf(os.Stderr, "\033[2K\r%s", line)
					last = line
				}
			case <-stopProgress:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(stopProgress)
	waiting.Wait()
	if err != nil {
		fmt.Fprint(os.Stderr, "\033[2K\r")
		return fmt.Errorf("Music.appからジャケ写を取得できませんでした: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\033[2K\rジャケ写を取得完了（%d曲を確認）\n", total)
	for playlistIndex := range playlists {
		for trackIndex := range playlists[playlistIndex].Tracks {
			path := filepath.Join(artworkDir, fmt.Sprintf("%d-%d.jpg", playlistIndex+1, trackIndex+1))
			if _, err := os.Stat(path); err == nil {
				playlists[playlistIndex].Tracks[trackIndex].Artwork = path
			}
		}
	}
	return nil
}

func filterPlaylists(all []Playlist, names []string) []Playlist {
	if len(names) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	var result []Playlist
	for _, p := range all {
		if want[p.Name] {
			result = append(result, p)
		}
	}
	return result
}

func runPlaylists(argv []string) error {
	fs := flag.NewFlagSet("playlists", flag.ContinueOnError)
	source := fs.String("source-json", "", "JSON source")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	playlists, err := loadPlaylists(*source, true, nil)
	if err != nil {
		return err
	}
	for _, p := range playlists {
		count := p.TrackCount
		if count == 0 {
			count = len(p.Tracks)
		}
		fmt.Printf("%s\t%d曲\n", p.Name, count)
	}
	return nil
}
