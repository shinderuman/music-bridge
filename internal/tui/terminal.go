package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

type Terminal struct {
	input  io.Reader
	output io.Writer
	raw    func() (func(), error)
}

func System() *Terminal {
	return &Terminal{
		input:  os.Stdin,
		output: os.Stdout,
		raw:    systemRaw,
	}
}

func New(input io.Reader, output io.Writer) *Terminal {
	return &Terminal{
		input:  input,
		output: output,
		raw:    func() (func(), error) { return func() {}, nil },
	}
}

func (terminal *Terminal) SelectOne(count int, title string, label func(int) string) (int, error) {
	if count == 0 {
		return 0, fmt.Errorf("選択肢がありません")
	}
	restore, err := terminal.raw()
	if err != nil {
		return 0, err
	}
	defer restore()
	index := 0
	for {
		fmt.Fprint(terminal.output, "\033[2J\033[H", title, "\r\n")
		for item := 0; item < count; item++ {
			cursor := " "
			if item == index {
				cursor = "▶"
			}
			fmt.Fprintf(terminal.output, "%s %s\r\n", cursor, label(item))
		}
		fmt.Fprint(terminal.output, "\r\n↑↓:移動  Enter:決定  q:中止\r\n")
		key, err := terminal.readKey()
		if err != nil {
			return 0, err
		}
		switch key {
		case "\033[A", "k":
			index = (index - 1 + count) % count
		case "\033[B", "j":
			index = (index + 1) % count
		case "\r", "\n":
			return index, nil
		case "q":
			return 0, fmt.Errorf("ユーザーにより中断しました")
		}
	}
}

func (terminal *Terminal) SelectMany(
	count int,
	selected map[int]bool,
	warning string,
	label func(int) string,
) ([]int, error) {
	if count == 0 {
		return nil, fmt.Errorf("選択肢がありません")
	}
	restore, err := terminal.raw()
	if err != nil {
		return nil, err
	}
	defer restore()
	index := 0
	for {
		fmt.Fprint(terminal.output, "\033[2J\033[Hプレイリストを選択してください\r\n")
		if warning != "" {
			fmt.Fprint(terminal.output, "\r\n", warning, "\r\n")
		}
		for item := 0; item < count; item++ {
			cursor := " "
			if item == index {
				cursor = "▶"
			}
			mark := "[ ]"
			if selected[item] {
				mark = "[x]"
			}
			fmt.Fprintf(terminal.output, "%s %s %s\r\n", cursor, mark, label(item))
		}
		fmt.Fprint(terminal.output, "\r\n↑↓:移動  Space:選択  a:全選択  Enter:決定  q:中止\r\n")
		key, err := terminal.readKey()
		if err != nil {
			return nil, err
		}
		switch key {
		case "\033[A", "k":
			index = (index - 1 + count) % count
		case "\033[B", "j":
			index = (index + 1) % count
		case " ", "　":
			selected[index] = !selected[index]
		case "a":
			for item := 0; item < count; item++ {
				selected[item] = true
			}
		case "\r", "\n":
			result := selectedIndices(count, selected)
			if len(result) == 0 {
				return nil, fmt.Errorf("プレイリストが選択されていません")
			}
			return result, nil
		case "q":
			return nil, fmt.Errorf("ユーザーにより中断しました")
		}
	}
}

func selectedIndices(count int, selected map[int]bool) []int {
	result := make([]int, 0, len(selected))
	for index := 0; index < count; index++ {
		if selected[index] {
			result = append(result, index)
		}
	}
	return result
}

func (terminal *Terminal) readKey() (string, error) {
	first := make([]byte, 1)
	if _, err := io.ReadFull(terminal.input, first); err != nil {
		return "", err
	}
	if first[0] == 27 {
		rest := make([]byte, 2)
		if _, err := io.ReadFull(terminal.input, rest); err != nil {
			return "", err
		}
		return string(append(first, rest...)), nil
	}
	size := 1
	switch {
	case first[0]&0xE0 == 0xC0:
		size = 2
	case first[0]&0xF0 == 0xE0:
		size = 3
	case first[0]&0xF8 == 0xF0:
		size = 4
	}
	if size == 1 || !utf8.FullRune(first) {
		if first[0] < 0x80 {
			return string(first), nil
		}
	}
	if size == 1 {
		return string(first), nil
	}
	rest := make([]byte, size-1)
	if _, err := io.ReadFull(terminal.input, rest); err != nil {
		return "", err
	}
	return string(append(first, rest...)), nil
}

func systemRaw() (func(), error) {
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
