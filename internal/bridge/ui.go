package bridge

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func chooseSyncMode() (syncMode, error) {
	items := []string{
		"ドライブ更新モード（Macに接続したmicroSDXCなど）",
		"Android更新モード（Wireless debugging）",
	}
	index, err := interactiveOne(items, "更新モードを選択してください", func(index int) string {
		return items[index]
	})
	if err != nil {
		return "", err
	}
	if index == 1 {
		return androidSyncMode, nil
	}
	return driveSyncMode, nil
}

func chooseMany(playlists []Playlist, root string) ([]Playlist, error) {
	existing := map[string]bool{}
	if inventory, err := scanPlaylistInventory(root); err == nil {
		for _, playlist := range playlists {
			if inventory.contains(playlist.Name) {
				existing[safeName(playlist.Name)] = true
			}
		}
	}
	return chooseManyWithExisting(playlists, existing)
}

func chooseManyWithExisting(playlists []Playlist, existing map[string]bool) ([]Playlist, error) {
	if len(playlists) == 0 {
		return nil, fmt.Errorf("プレイリストがありません")
	}
	selected := map[int]bool{}
	duplicates := duplicatePlaylistNames(playlists)
	for i, p := range playlists {
		if existing[safeName(p.Name)] {
			selected[i] = true
		}
	}
	items := func() []Playlist {
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
		warning = "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\r\n! 警告: 同名プレイリストが存在します                 !\r\n! このアプリは同名プレイリストに対応していません。   !\r\n! 名前で区別できないため、正しく同期できません。     !\r\n!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
	}
	return interactiveMany(playlists, selected, warning, func(i int) string {
		p := playlists[i]
		count := p.TrackCount
		if count == 0 {
			count = len(p.Tracks)
		}
		return fmt.Sprintf("%s (%d曲)", p.Name, count)
	}, items)
}

func duplicatePlaylistNames(playlists []Playlist) map[string]bool {
	seen := map[string]bool{}
	duplicates := map[string]bool{}
	for _, playlist := range playlists {
		name := safeName(playlist.Name)
		key := portablePathKey(name)
		if seen[key] {
			duplicates[name] = true
		}
		seen[key] = true
	}
	return duplicates
}

func chooseTarget() (string, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return "", err
	}
	volumes := []string{}
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
	get := exec.Command("stty", "-g")
	get.Stdin = os.Stdin
	state, err := get.Output()
	if err != nil {
		return nil, err
	}
	set := exec.Command("stty", "raw", "-echo")
	set.Stdin = os.Stdin
	if err := set.Run(); err != nil {
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
	if size == 1 || !utf8.FullRune([]byte{b[0]}) {
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
