package musicapp

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type artworkCacheState int

const (
	artworkNotCached artworkCacheState = iota
	artworkCached
	artworkMissing
)

var artworkCacheLocation = artworkCachePath
var artworkMusicExporter = exportArtworksFromMusic

func artworkCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Music Bridge", "artwork"), nil
}

func artworkCacheKeyPath(root, key, extension string) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	return filepath.Join(root, hash[:2], hash+extension)
}

func lookupArtworkCache(root, key string) (string, artworkCacheState) {
	image := artworkCacheKeyPath(root, key, ".jpg")
	if info, err := os.Stat(image); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return image, artworkCached
	}
	missing := artworkCacheKeyPath(root, key, ".none")
	if info, err := os.Stat(missing); err == nil && info.Mode().IsRegular() {
		return "", artworkMissing
	}
	return "", artworkNotCached
}

func storeArtworkImage(root, key, source string) (string, error) {
	destination := artworkCacheKeyPath(root, key, ".jpg")
	if image, state := lookupArtworkCache(root, key); state == artworkCached {
		return image, nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".artwork-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return "", err
	}
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return "", err
	}
	_ = os.Remove(artworkCacheKeyPath(root, key, ".none"))
	return destination, nil
}

func storeMissingArtwork(root, key string) error {
	if _, state := lookupArtworkCache(root, key); state == artworkCached {
		return nil
	}
	destination := artworkCacheKeyPath(root, key, ".none")
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".missing-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := fmt.Fprintln(temp, key); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, destination)
}

func applyCachedArtwork(playlists []Playlist, requests []ArtworkRequest, root string) ([]ArtworkRequest, int) {
	stateByAlbum := map[string]artworkCacheState{}
	pathByAlbum := map[string]string{}
	var pending []ArtworkRequest
	for _, request := range requests {
		state, checked := stateByAlbum[request.AlbumKey]
		if !checked {
			pathByAlbum[request.AlbumKey], state = lookupArtworkCache(root, request.AlbumKey)
			stateByAlbum[request.AlbumKey] = state
		}
		if state == artworkNotCached {
			pending = append(pending, request)
			continue
		}
		if state == artworkCached {
			playlists[request.PlaylistIndex-1].Tracks[request.TrackIndex-1].Artwork = pathByAlbum[request.AlbumKey]
		}
	}
	return pending, len(stateByAlbum) - countPendingArtworkAlbums(pending)
}

func countPendingArtworkAlbums(requests []ArtworkRequest) int {
	albums := map[string]bool{}
	for _, request := range requests {
		albums[request.AlbumKey] = true
	}
	return len(albums)
}

func persistArtworkResults(playlists []Playlist, requests []ArtworkRequest, temporaryDir, cacheRoot string) (int, int, error) {
	requestsByAlbum := map[string][]ArtworkRequest{}
	for _, request := range requests {
		requestsByAlbum[request.AlbumKey] = append(requestsByAlbum[request.AlbumKey], request)
	}
	images, missing := 0, 0
	for album, albumRequests := range requestsByAlbum {
		var source string
		for _, request := range albumRequests {
			candidate := filepath.Join(temporaryDir, fmt.Sprintf("%d-%d.jpg", request.PlaylistIndex, request.TrackIndex))
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				source = candidate
				break
			}
		}
		if source == "" {
			if err := storeMissingArtwork(cacheRoot, album); err != nil {
				return images, missing, err
			}
			missing++
			continue
		}
		cached, err := storeArtworkImage(cacheRoot, album, source)
		if err != nil {
			return images, missing, err
		}
		images++
		for _, request := range albumRequests {
			playlists[request.PlaylistIndex-1].Tracks[request.TrackIndex-1].Artwork = cached
		}
	}
	return images, missing, nil
}
