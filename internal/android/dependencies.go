package android

import (
	"fmt"
	"os"
	"strings"

	"music-bridge/internal/diagnostic"
	"music-bridge/internal/layout"
	"music-bridge/internal/library"
	"music-bridge/internal/musicapp"
	"music-bridge/internal/notify"
	"music-bridge/internal/playlistfile"
	"music-bridge/internal/playlistselect"
	"music-bridge/internal/portable"
	"music-bridge/internal/targetlock"
	"music-bridge/internal/textfmt"
	"music-bridge/internal/tui"
)

type Track = library.Track
type Playlist = library.Playlist
type Planned = library.Planned
type artworkRequest = musicapp.ArtworkRequest

const (
	dataDir                     = layout.DataDirectory
	libraryDir                  = layout.LibraryDirectory
	marker                      = layout.TargetMarker
	manifest                    = layout.Manifest
	pendingManifest             = layout.PendingManifest
	albumArtworkFile            = layout.AlbumArtwork
	libraryManifestMarker       = layout.LibraryManifestMarker
	legacyArtworkManifestMarker = ".music-bridge-artwork-indexed"
)

var terminalUI = tui.System()
var androidNotifier = notify.New(os.Stderr)

func interactiveOne(items []string, title string, label func(int) string) (int, error) {
	return terminalUI.SelectOne(len(items), title, label)
}

func chooseManyWithExisting(playlists []Playlist, existing map[string]bool) ([]Playlist, error) {
	return playlistselect.Select(terminalUI, playlists, existing)
}

func safeName(value string) string {
	return playlistfile.SafeName(value)
}

func renderPlaylist(playlist Playlist, available map[string]string) []byte {
	return playlistfile.Render(playlist, available)
}

func lockTarget(root string) (func(), error) { return targetlock.Lock(root) }

func confirmDefaultYes(prompt string) bool {
	fmt.Print(prompt)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func humanBytes(bytes int64) string {
	return textfmt.HumanBytes(bytes)
}

func truncateRunes(value string, limit int) string {
	return textfmt.TruncateRunes(value, limit)
}

func loadPlaylists(source string, summary bool, names []string) ([]Playlist, error) {
	return musicapp.LoadPlaylists(source, summary, names)
}

func loadSyncPlaylists(source string, selected []Playlist, refresh bool) ([]Playlist, error) {
	return musicapp.LoadSyncPlaylists(source, selected, refresh)
}

func makePlan(playlists []Playlist) ([]Planned, []string, error) {
	return library.MakePlan(playlists)
}

func countTracks(playlists []Playlist) int { return library.CountTracks(playlists) }

func validatePlan(plan []Planned, playlists []Playlist) error {
	return library.ValidatePlan(plan, playlists)
}

func artworkRequests(playlists []Playlist, plan []Planned) []artworkRequest {
	return musicapp.ArtworkRequests(playlists, plan)
}

func exportArtworks(playlists []Playlist, artworkDir string, requests []artworkRequest) error {
	return musicapp.ExportArtworks(playlists, artworkDir, requests)
}

func portablePathKey(value string) string        { return portable.Key(value) }
func androidVisiblePath(value string) string     { return portable.AndroidVisible(value) }
func logicalPathFromAndroid(value string) string { return portable.LogicalFromAndroid(value) }
func isAppleDoublePath(value string) bool        { return portable.IsAppleDouble(value) }

func setDiagnosticLogContext(context string) error { return diagnostic.Default.SetContext(context) }
func logf(format string, args ...any)              { diagnostic.Default.Printf(format, args...) }
