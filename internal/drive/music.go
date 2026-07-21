package drive

import "music-bridge/internal/musicapp"

type artworkRequest = musicapp.ArtworkRequest

func loadPlaylists(source string, summary bool, names []string) ([]Playlist, error) {
	return musicapp.LoadPlaylists(source, summary, names)
}

func loadSyncPlaylists(source string, selected []Playlist, refresh bool) ([]Playlist, error) {
	return musicapp.LoadSyncPlaylists(source, selected, refresh)
}

func artworkRequests(playlists []Playlist, plan []Planned) []artworkRequest {
	return musicapp.ArtworkRequests(playlists, plan)
}

func artworkCandidateDirs(plan []Planned, root string) map[string]bool {
	return musicapp.ArtworkCandidateDirs(plan, root)
}

func exportArtworks(playlists []Playlist, artworkDir string, requests []artworkRequest) error {
	return musicapp.ExportArtworks(playlists, artworkDir, requests)
}
