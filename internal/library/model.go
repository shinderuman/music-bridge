package library

const Directory = "Library"

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

type Planned struct {
	Track    Track
	Relative string
	Size     int64
}

func CountTracks(playlists []Playlist) int {
	total := 0
	for _, playlist := range playlists {
		total += len(playlist.Tracks)
	}
	return total
}

func TotalBytes(plan []Planned) int64 {
	var total int64
	for _, item := range plan {
		total += item.Size
	}
	return total
}
