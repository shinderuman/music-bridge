package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const dataDir = "music-bridge"
const marker = ".music-bridge-target"

type Track struct {
	Name        string `json:"name"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"album_artist"`
	Album       string `json:"album"`
	Location    string `json:"location"`
}

type Playlist struct {
	Name       string  `json:"name"`
	TrackCount int     `json:"trackCount,omitempty"`
	Tracks     []Track `json:"tracks,omitempty"`
}

type Planned struct {
	Track    Track
	Relative string
	Size     int64
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "playlists":
		if err := runPlaylists(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "sync":
		if err := runSync(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("usage: music-bridge {playlists|sync} [--target PATH] [--init-target] [--dry-run] [--yes]")
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "music-bridge:", err); os.Exit(1) }

func sourceArgs(source string, summary bool, names []string) ([]string, error) {
	if source == "" {
		source = "mvp/python/scripts/export_music_library.js"
	}
	args := []string{"-l", "JavaScript", source}
	if summary {
		args = append(args, "--summary")
	}
	for _, name := range names {
		args = append(args, "--playlist", name)
	}
	return args, nil
}

func loadPlaylists(source string, summary bool, names []string) ([]Playlist, error) {
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
	args, _ := sourceArgs(source, summary, names)
	cmd := exec.Command("osascript", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Music.appから取得できませんでした: %w", err)
	}
	var playlists []Playlist
	if err := json.Unmarshal(out, &playlists); err != nil {
		return nil, fmt.Errorf("取得結果がJSONではありません: %w", err)
	}
	return playlists, nil
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

func chooseMany(playlists []Playlist, root string) ([]Playlist, error) {
	if len(playlists) == 0 {
		return nil, fmt.Errorf("プレイリストがありません")
	}
	selected := map[int]bool{}
	seenNames := map[string]bool{}
	duplicates := map[string]bool{}
	for _, p := range playlists {
		name := safeName(p.Name)
		if seenNames[name] {
			duplicates[name] = true
		}
		seenNames[name] = true
	}
	for i, p := range playlists {
		path := filepath.Join(root, "Music", safeName(p.Name)+".m3u")
		if _, err := os.Stat(path); err == nil {
			selected[i] = true
		}
	}
	returnItems := func() []Playlist {
		result := []Playlist{}
		for i, p := range playlists {
			if selected[i] {
				result = append(result, p)
			}
		}
		return result
	}
	warning := ""
	if len(duplicates) > 0 {
		warning = "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\r\n" +
			"! 警告: 同名プレイリストが存在します                 !\r\n" +
			"! このアプリは同名プレイリストに対応していません。   !\r\n" +
			"! 名前で区別できないため、正しく同期できません。     !\r\n" +
			"!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
	}
	return interactiveMany(playlists, selected, warning, func(i int) string {
		p := playlists[i]
		count := p.TrackCount
		if count == 0 {
			count = len(p.Tracks)
		}
		return fmt.Sprintf("%s (%d曲)", p.Name, count)
	}, returnItems)
}

func chooseTarget(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return "", err
	}
	var volumes []string
	for _, e := range entries {
		if e.IsDir() {
			volumes = append(volumes, filepath.Join("/Volumes", e.Name()))
		}
	}
	sort.Strings(volumes)
	if len(volumes) == 0 {
		return "", fmt.Errorf("/Volumesに同期先がありません")
	}
	index, err := interactiveOne(volumes, "同期先を選択してください", func(i int) string { return volumes[i] })
	if err != nil {
		return "", err
	}
	return volumes[index], nil
}

func terminalRaw() (func(), error) {
	getState := exec.Command("stty", "-g")
	getState.Stdin = os.Stdin
	state, err := getState.Output()
	if err != nil {
		return nil, err
	}
	setRaw := exec.Command("stty", "raw", "-echo")
	setRaw.Stdin = os.Stdin
	if err := setRaw.Run(); err != nil {
		return nil, err
	}
	return func() {
		restore := exec.Command("stty", strings.TrimSpace(string(state)))
		restore.Stdin = os.Stdin
		_ = restore.Run()
	}, nil
}

func key() (string, error) {
	b := make([]byte, 1)
	if _, err := os.Stdin.Read(b); err != nil {
		return "", err
	}
	if b[0] == 27 {
		rest := make([]byte, 2)
		if _, err := io.ReadFull(os.Stdin, rest); err != nil {
			return "", err
		}
		return string(append(b, rest...)), nil
	}
	size := 1
	switch {
	case b[0]&0xE0 == 0xC0:
		size = 2
	case b[0]&0xF0 == 0xE0:
		size = 3
	case b[0]&0xF8 == 0xF0:
		size = 4
	}
	if size == 1 || !utf8.FullRune(append([]byte(nil), b...)) {
		// ASCIIキーはそのまま返す。マルチバイト文字は下で残りを読む。
		if b[0] < 0x80 {
			return string(b), nil
		}
	}
	if size == 1 {
		return string(b), nil
	}
	rest := make([]byte, size-1)
	if _, err := io.ReadFull(os.Stdin, rest); err != nil {
		return "", err
	}
	return string(append(b, rest...)), nil
}

func interactiveOne(items []string, title string, label func(int) string) (int, error) {
	restore, err := terminalRaw()
	if err != nil {
		return 0, err
	}
	defer restore()
	index := 0
	for {
		fmt.Print("\033[2J\033[H", title, "\r\n")
		for i := range items {
			cursor := " "
			if i == index {
				cursor = "▶"
			}
			fmt.Printf("%s %s\r\n", cursor, label(i))
		}
		fmt.Print("\r\n↑↓:移動  Enter:決定  q:中止\r\n")
		k, err := key()
		if err != nil {
			return 0, err
		}
		switch k {
		case "\033[A", "k":
			index = (index - 1 + len(items)) % len(items)
		case "\033[B", "j":
			index = (index + 1) % len(items)
		case "\r", "\n":
			return index, nil
		case "q":
			return 0, fmt.Errorf("ユーザーにより中断しました")
		}
	}
}

func interactiveMany(items []Playlist, selected map[int]bool, warning string, label func(int) string, result func() []Playlist) ([]Playlist, error) {
	restore, err := terminalRaw()
	if err != nil {
		return nil, err
	}
	defer restore()
	index := 0
	for {
		fmt.Print("\033[2J\033[Hプレイリストを選択してください\r\n")
		if warning != "" {
			fmt.Print("\r\n", warning, "\r\n")
		}
		for i := range items {
			cursor := " "
			if i == index {
				cursor = "▶"
			}
			mark := "[ ]"
			if selected[i] {
				mark = "[x]"
			}
			fmt.Printf("%s %s %s\r\n", cursor, mark, label(i))
		}
		fmt.Print("\r\n↑↓:移動  Space:選択  a:全選択  Enter:決定  q:中止\r\n")
		k, err := key()
		if err != nil {
			return nil, err
		}
		switch k {
		case "\033[A", "k":
			index = (index - 1 + len(items)) % len(items)
		case "\033[B", "j":
			index = (index + 1) % len(items)
		case " ", "　":
			selected[index] = !selected[index]
		case "a":
			for i := range items {
				selected[i] = true
			}
		case "\r", "\n":
			if len(result()) == 0 {
				return nil, fmt.Errorf("プレイリストが選択されていません")
			}
			return result(), nil
		case "q":
			return nil, fmt.Errorf("ユーザーにより中断しました")
		}
	}
}

func runSync(argv []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	target := fs.String("target", "", "target volume")
	initTarget := fs.Bool("init-target", false, "initialize target")
	dryRun := fs.Bool("dry-run", false, "dry run")
	yes := fs.Bool("yes", false, "skip confirmations")
	source := fs.String("source-json", "", "JSON source")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	volume, err := chooseTarget(*target)
	if err != nil {
		return err
	}
	root := filepath.Join(volume, dataDir)
	markerPath := filepath.Join(root, marker)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		if !*initTarget {
			fmt.Printf("%sを初期化しますか？ [y/N] ", root)
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(answer) != "y" {
				return fmt.Errorf("同期先の初期化をキャンセルしました")
			}
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(markerPath, []byte("Music Bridge target\n"), 0644); err != nil {
			return err
		}
	}
	summaries, err := loadPlaylists(*source, true, nil)
	if err != nil {
		return err
	}
	selected, err := chooseMany(summaries, root)
	if err != nil {
		return err
	}
	names := make([]string, len(selected))
	for i, p := range selected {
		names[i] = p.Name
	}
	playlists, err := loadPlaylists(*source, false, names)
	if err != nil {
		return err
	}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		fmt.Printf("ローカルファイルなし: %d曲\n", len(missing))
	}
	required, err := existingBytes(plan, root)
	if err != nil {
		return err
	}
	free, err := freeBytes(volume)
	if err != nil {
		return err
	}
	fmt.Printf("選択プレイリスト: %d件 / 曲: %d曲\n", len(playlists), countTracks(playlists))
	fmt.Printf("新規転送容量: %s / 空き容量: %s\n", humanBytes(required), humanBytes(free))
	if required > free {
		fmt.Print("容量不足です。空き容量の範囲で続行しますか？ [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			return fmt.Errorf("容量不足のため中断しました")
		}
		plan = fitPlan(plan, root, free)
	} else if !*yes {
		fmt.Print("同期を開始しますか？ [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			return fmt.Errorf("ユーザーにより中断しました")
		}
	}
	stale := stalePlaylists(summaries, selected, root)
	if len(stale) > 0 {
		fmt.Println("警告: 選択されなかったプレイリストのM3Uを削除します:")
		for _, path := range stale {
			fmt.Println("  削除:", path)
		}
		if !*yes {
			fmt.Print("削除して続行しますか？ [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(answer) != "y" {
				return fmt.Errorf("プレイリスト削除をキャンセルしました")
			}
		}
	}
	labels := map[string]string{}
	for _, p := range playlists {
		for _, t := range p.Tracks {
			if t.Location != "" && labels[t.Location] == "" {
				labels[t.Location] = p.Name
			}
		}
	}
	if err := transfer(plan, root, *dryRun, labels); err != nil {
		return err
	}
	if err := writePlaylists(playlists, plan, root, *dryRun); err != nil {
		return err
	}
	if !*dryRun {
		for _, path := range stale {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	fmt.Printf("転送完了: %d/%d曲\n同期完了: %dプレイリスト\n", len(plan), len(plan), len(playlists))
	return nil
}

func countTracks(playlists []Playlist) int {
	total := 0
	for _, p := range playlists {
		total += len(p.Tracks)
	}
	return total
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
			rel := filepath.Join(artist, t.Album, filepath.Base(t.Location))
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

func freeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func sameFile(source, destination string) bool {
	a, err := os.Open(source)
	if err != nil {
		return false
	}
	defer a.Close()
	b, err := os.Open(destination)
	if err != nil {
		return false
	}
	defer b.Close()
	ai, err := a.Stat()
	if err != nil {
		return false
	}
	bi, err := b.Stat()
	if err != nil || ai.Size() != bi.Size() {
		return false
	}
	bufA, bufB := make([]byte, 1024*1024), make([]byte, 1024*1024)
	for {
		na, ea := a.Read(bufA)
		nb, eb := b.Read(bufB)
		if na != nb {
			return false
		}
		for i := 0; i < na; i++ {
			if bufA[i] != bufB[i] {
				return false
			}
		}
		if ea == io.EOF && eb == io.EOF {
			return true
		}
		if ea != nil || eb != nil {
			return false
		}
	}
}

func existingBytes(plan []Planned, root string) (int64, error) {
	var total int64
	for _, p := range plan {
		destination := filepath.Join(root, "Music", p.Relative)
		if !sameFile(p.Track.Location, destination) {
			total += p.Size
		}
	}
	return total, nil
}

func fitPlan(plan []Planned, root string, free int64) []Planned {
	result := []Planned{}
	for _, p := range plan {
		destination := filepath.Join(root, "Music", p.Relative)
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

func transfer(plan []Planned, root string, dry bool, labels map[string]string) error {
	started := time.Now()
	total := totalBytes(plan)
	var done int64
	for i, p := range plan {
		dest := filepath.Join(root, "Music", filepath.Dir(p.Relative))
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		args := []string{"-ah", "--partial", "--append-verify"}
		if dry {
			args = append(args, "--dry-run")
		}
		args = append(args, p.Track.Location, dest+string(os.PathSeparator))
		if err := exec.Command("rsync", args...).Run(); err != nil {
			return err
		}
		done += p.Size
		rate := float64(done) / time.Since(started).Seconds()
		eta := time.Duration(float64(total-done)/rate) * time.Second
		label := labels[p.Track.Location]
		if label != "" {
			label = " | プレイリスト: " + label
		}
		fmt.Printf("転送中 [%d/%d] %5.1f%%%s | %s | ETA %s\r", i+1, len(plan), float64(i+1)*100/float64(len(plan)), label, p.Track.Name, eta.Round(time.Second))
	}
	fmt.Println()
	return nil
}

func writePlaylists(playlists []Playlist, plan []Planned, root string, dry bool) error {
	available := map[string]bool{}
	for _, item := range plan {
		available[item.Track.Location] = true
	}
	for i, p := range playlists {
		fmt.Printf("プレイリスト生成中 [%d/%d] %s\n", i+1, len(playlists), p.Name)
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		for _, t := range p.Tracks {
			if t.Location == "" || !available[t.Location] {
				continue
			}
			artist := t.AlbumArtist
			if artist == "" {
				artist = t.Artist
			}
			b.WriteString(filepath.ToSlash(filepath.Join(artist, t.Album, filepath.Base(t.Location))))
			b.WriteByte('\n')
		}
		if !dry {
			path := filepath.Join(root, "Music", safeName(p.Name)+".m3u")
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
	entries, err := os.ReadDir(filepath.Join(root, "Music"))
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
			result = append(result, filepath.Join(root, "Music", e.Name()))
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
