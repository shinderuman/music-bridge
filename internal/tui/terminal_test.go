package tui

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestSelectOneMovesAndWraps(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"\033[B\r", 1},
		{"k\r", 2},
	}
	for _, test := range tests {
		var output bytes.Buffer
		terminal := New(strings.NewReader(test.input), &output)
		got, err := terminal.SelectOne(3, "title", func(index int) string { return string(rune('A' + index)) })
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("SelectOne(%q)=%d, want %d", test.input, got, test.want)
		}
		if !strings.Contains(output.String(), "title") {
			t.Fatalf("output=%q", output.String())
		}
	}
}

func TestSelectManyAcceptsHalfAndFullWidthSpace(t *testing.T) {
	var output bytes.Buffer
	terminal := New(strings.NewReader(" \033[B　\r"), &output)
	got, err := terminal.SelectMany(3, map[int]bool{}, "warning", func(index int) string {
		return string(rune('A' + index))
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%#v, want %#v", got, want)
	}
	if !strings.Contains(output.String(), "warning") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestSelectManySelectsAllAndRejectsEmptySelection(t *testing.T) {
	terminal := New(strings.NewReader("a\r"), &bytes.Buffer{})
	got, err := terminal.SelectMany(2, map[int]bool{}, "", func(int) string { return "item" })
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%#v, want %#v", got, want)
	}

	empty := New(strings.NewReader("\r"), &bytes.Buffer{})
	if _, err := empty.SelectMany(1, map[int]bool{}, "", func(int) string { return "item" }); err == nil {
		t.Fatal("empty selection was accepted")
	}
}

func TestSelectionCanBeCancelled(t *testing.T) {
	terminal := New(strings.NewReader("q"), &bytes.Buffer{})
	if _, err := terminal.SelectOne(1, "title", func(int) string { return "item" }); err == nil {
		t.Fatal("cancel was accepted")
	}
}
