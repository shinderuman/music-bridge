package musicapp

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type fakeScriptCommand struct {
	start func() error
	wait  func() error
}

func (command fakeScriptCommand) Start() error {
	if command.start == nil {
		return nil
	}
	return command.start()
}

func (command fakeScriptCommand) Wait() error {
	if command.wait == nil {
		return nil
	}
	return command.wait()
}

func replaceScriptCommand(t *testing.T, factory func(string, []string, io.Writer, io.Writer) scriptCommand) {
	t.Helper()
	previous := newScriptCommand
	newScriptCommand = factory
	t.Cleanup(func() { newScriptCommand = previous })
}

func TestLoadPlaylistsRunsOsascriptAndDecodesOutput(t *testing.T) {
	var commandName string
	var commandArgs []string
	replaceScriptCommand(t, func(name string, args []string, stdout, _ io.Writer) scriptCommand {
		commandName = name
		commandArgs = append([]string(nil), args...)
		return fakeScriptCommand{start: func() error {
			_, err := io.WriteString(stdout, `[{"name":"GAME","trackCount":2}]`)
			return err
		}}
	})

	got, err := LoadPlaylists("", true, []string{"GAME"})
	if err != nil {
		t.Fatal(err)
	}
	if commandName != "osascript" {
		t.Fatalf("command = %q", commandName)
	}
	wantArgs := []string{
		"-l", "JavaScript", filepath.Join("scripts", "export_music_library.js"),
		"--summary", "--playlist", "GAME",
	}
	if !reflect.DeepEqual(commandArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", commandArgs, wantArgs)
	}
	if len(got) != 1 || got[0].Name != "GAME" || got[0].TrackCount != 2 {
		t.Fatalf("playlists = %#v", got)
	}
}

func TestLoadPlaylistsReportsCommandStartAndWaitFailures(t *testing.T) {
	tests := []struct {
		name string
		cmd  fakeScriptCommand
		want string
	}{
		{name: "start", cmd: fakeScriptCommand{start: func() error { return errors.New("start failed") }}, want: "Music.appを起動できませんでした"},
		{name: "wait", cmd: fakeScriptCommand{wait: func() error { return errors.New("wait failed") }}, want: "Music.appから取得できませんでした"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			replaceScriptCommand(t, func(string, []string, io.Writer, io.Writer) scriptCommand {
				return test.cmd
			})
			_, err := LoadPlaylists("", true, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExportArtworksRunsOsascriptAndAppliesGeneratedImage(t *testing.T) {
	temporaryDir := t.TempDir()
	playlists := []Playlist{{Name: "P", Tracks: []Track{{Name: "Song"}}}}
	requests := []ArtworkRequest{{PlaylistIndex: 1, TrackIndex: 1, AlbumKey: "Library/A/Album"}}
	var commandName string
	var commandArgs []string
	replaceScriptCommand(t, func(name string, args []string, _, _ io.Writer) scriptCommand {
		commandName = name
		commandArgs = append([]string(nil), args...)
		return fakeScriptCommand{start: func() error {
			return os.WriteFile(filepath.Join(temporaryDir, "1-1.jpg"), []byte("image"), 0600)
		}}
	})

	if err := exportArtworksFromMusic(playlists, temporaryDir, requests); err != nil {
		t.Fatal(err)
	}
	if commandName != "osascript" {
		t.Fatalf("command = %q", commandName)
	}
	if len(commandArgs) < 5 || commandArgs[0] != filepath.Join("scripts", "export_music_artwork.applescript") || commandArgs[len(commandArgs)-1] != "P" {
		t.Fatalf("args = %#v", commandArgs)
	}
	want := filepath.Join(temporaryDir, "1-1.jpg")
	if playlists[0].Tracks[0].Artwork != want {
		t.Fatalf("artwork = %q, want %q", playlists[0].Tracks[0].Artwork, want)
	}
}

func TestMusicAppIntegrationListsPlaylists(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Music.app integration is only available on macOS")
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript is not available")
	}
	musicAvailable := false
	for _, application := range []string{
		"/System/Applications/Music.app",
		"/Applications/Music.app",
	} {
		if info, err := os.Stat(application); err == nil && info.IsDir() {
			musicAvailable = true
			break
		}
	}
	if !musicAvailable {
		t.Skip("Music.app is not installed")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	script := filepath.Join(repositoryRoot, "scripts", "export_music_library.js")
	if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
		t.Skip("Music.app integration script is not available")
	}
	previousExecutablePath := executablePath
	executablePath = func() (string, error) {
		return filepath.Join(repositoryRoot, "music-bridge-test"), nil
	}
	t.Cleanup(func() { executablePath = previousExecutablePath })

	playlists, err := LoadPlaylists("", true, nil)
	if err != nil {
		t.Fatalf("Music.app integration failed: %v", err)
	}
	if len(playlists) == 0 {
		t.Fatal("Music.app returned no playlists")
	}
}
