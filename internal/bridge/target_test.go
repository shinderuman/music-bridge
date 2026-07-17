package bridge

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(a, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("same files were not recognized")
	}
	if err := os.WriteFile(b, []byte("diff"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("same metadata should be treated as unchanged")
	}
	if err := os.Chtimes(b, stamp.Add(1500*time.Millisecond), stamp.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, b) {
		t.Fatal("small filesystem timestamp precision differences should be ignored")
	}
	if err := os.Chtimes(b, stamp.Add(3*time.Second), stamp.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if sameFile(a, b) {
		t.Fatal("large timestamp differences should not be ignored")
	}
}

func TestLockTargetRejectsConcurrentSyncForSameTarget(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockTarget(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if _, err := lockTarget(root); err == nil {
		t.Fatal("second lock for the same target succeeded")
	}
	unlock()
	unlockAgain, err := lockTarget(root)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	unlockAgain()
}

func TestLockTargetIsReleasedAfterSIGINT(t *testing.T) {
	root := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestLockTargetSignalHelper$")
	command.Env = append(os.Environ(), "MUSIC_BRIDGE_LOCK_TEST_ROOT="+root)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper output = %q, want locked", line)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("helper unexpectedly exited without SIGINT")
	}
	unlock, err := lockTarget(root)
	if err != nil {
		t.Fatalf("lock remained after SIGINT: %v", err)
	}
	unlock()
}

func TestLockTargetSignalHelper(t *testing.T) {
	root := os.Getenv("MUSIC_BRIDGE_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	unlock, err := lockTarget(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer unlock()
	fmt.Println("locked")
	select {}
}

func TestStaleAudioUsesManifestAndSelectedUnion(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Old/Album/old.m4a")
	keep := filepath.Join(dir, "Artist/Album/keep.m4a")
	for _, path := range []string{old, keep} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("song"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifest(dir, []Planned{{Relative: "Old/Album/old.m4a"}, {Relative: "Artist/Album/keep.m4a"}}); err != nil {
		t.Fatal(err)
	}
	stale, size := staleAudio([]Planned{{Relative: "Artist/Album/keep.m4a"}}, dir)
	if !reflect.DeepEqual(stale, []string{"Old/Album/old.m4a"}) || size != 4 {
		t.Fatalf("staleAudio = %#v, %d", stale, size)
	}
}

func TestMigrateLegacyLayout(t *testing.T) {
	dir := t.TempDir()
	legacyTrack := filepath.Join(dir, "Artist", "Album", "song.m4a")
	if err := os.MkdirAll(filepath.Dir(legacyTrack), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyTrack, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	playlistPath := filepath.Join(dir, "Playlist.m3u")
	if err := os.WriteFile(playlistPath, []byte("\xef\xbb\xbf#EXTM3U\nArtist/Album/song.m4a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(dir, []Planned{{Relative: "Artist/Album/song.m4a"}}); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyLayout(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, libraryDir, "Artist", "Album", "song.m4a")); err != nil {
		t.Fatalf("migrated audio missing: %v", err)
	}
	if _, err := os.Stat(legacyTrack); !os.IsNotExist(err) {
		t.Fatalf("legacy audio still exists: %v", err)
	}
	playlist, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(playlist), "\xef\xbb\xbf#EXTM3U\nLibrary/Artist/Album/song.m4a\n"; got != want {
		t.Fatalf("playlist = %q, want %q", got, want)
	}
	if got, want := loadManifest(dir), []string{"Library/Artist/Album/song.m4a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}
