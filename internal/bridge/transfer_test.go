package bridge

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("あいうえお", 20); got != "あいうえお" {
		t.Fatalf("short title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてと", 20); got != "あいうえおかきくけこさしすせそたちつてと" {
		t.Fatalf("20-rune title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてとな", 20); got != "あいうえおかきくけこさしすせそたちつてと…" {
		t.Fatalf("long title = %q", got)
	}
}

func TestTransferEstimateUsesCompletedTransferTime(t *testing.T) {
	rate, eta := transferEstimate(300, 100, 10*time.Second)
	if rate != 10 {
		t.Fatalf("rate = %d, want 10", rate)
	}
	if eta != 20*time.Second {
		t.Fatalf("eta = %s, want 20s", eta)
	}
	if rate, eta := transferEstimate(300, 100, 0); rate != 0 || eta != 0 {
		t.Fatalf("zero elapsed estimate = %d, %s; want 0, 0s", rate, eta)
	}
}

func TestRetryTransferUsesOneTwoThreeSecondBackoff(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := retryTransfer(func() error {
		attempts++
		if attempts <= maxTransferRetries {
			return os.ErrNotExist
		}
		return nil
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	}, func(int, time.Duration) {})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
}

func TestRetryTransferStopsAfterThreeRetries(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	want := os.ErrNotExist
	err := retryTransfer(func() error {
		attempts++
		return want
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	}, func(int, time.Duration) {})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if got := len(delays); got != maxTransferRetries {
		t.Fatalf("retry waits = %d, want %d", got, maxTransferRetries)
	}
}

func TestTransferProgressKeepsLastCompletedItemAndLiveElapsedRate(t *testing.T) {
	first := Planned{Relative: "Library/A/first.m4a", Size: 100}
	second := Planned{Relative: "Library/A/second.m4a", Size: 100}
	progress := &transferProgress{}
	progress.startBatch(first)
	item, done, processed, rate, _ := progress.snapshot()
	if item != first || done != 0 || processed != 0 || rate != 0 {
		t.Fatalf("initial progress = %#v, %d, %d, %d", item, done, processed, rate)
	}
	progress.complete(second, 1000, 5*time.Second, second.Size)
	item, done, processed, rate, eta := progress.snapshot()
	if item != second || done != 100 || processed != 1 || rate != 20 || eta != 45*time.Second {
		t.Fatalf("completed progress = %#v, %d, %d, %d, %s", item, done, processed, rate, eta)
	}
	item, _, _, _, _ = progress.snapshot()
	if item != second {
		t.Fatalf("spinner item changed to %#v, want %#v", item, second)
	}
}

func TestRsyncItemForOutput(t *testing.T) {
	item := Planned{Relative: "Library/Artist/Album/song.m4a"}
	items := map[string]Planned{item.Relative: item}
	if got, ok := rsyncItemForOutput(items, "Library/Artist/Album/song.m4a"); !ok || got != item {
		t.Fatalf("rsync output item = %#v, %v", got, ok)
	}
	if _, ok := rsyncItemForOutput(items, "Library/Artist/Album"); ok {
		t.Fatal("directory output must not be treated as a transferred track")
	}
}

func TestRsyncOutFormatListsTransferredFile(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	source := t.TempDir()
	target := t.TempDir()
	relative := filepath.Join("Library", "Artist", "Album", "song.m4a")
	path := filepath.Join(source, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("rsync", "-ahL", "--out-format=%n", source+string(os.PathSeparator), target+string(os.PathSeparator)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), filepath.ToSlash(relative)) {
		t.Fatalf("rsync output %q does not contain %q", output, relative)
	}
}

func TestTransferCopiesPendingFile(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	source := filepath.Join(t.TempDir(), "song.m4a")
	if err := os.WriteFile(source, []byte("song data"), 0644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	relative := filepath.Join("Library", "Artist", "Album", "song.m4a")
	plan := []Planned{{
		Track:    Track{Name: "song", Location: source},
		Relative: relative,
		Size:     int64(len("song data")),
	}}
	if err := transfer(plan, root, false, map[string]string{source: "Playlist"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "song data" {
		t.Fatalf("transferred data = %q", data)
	}
}
