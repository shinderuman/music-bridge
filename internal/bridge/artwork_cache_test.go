package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportArtworksCachesImagesAndMissingAlbums(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "artwork")
	previousLocation := artworkCacheLocation
	previousExporter := artworkMusicExporter
	artworkCacheLocation = func() (string, error) { return cacheRoot, nil }
	t.Cleanup(func() {
		artworkCacheLocation = previousLocation
		artworkMusicExporter = previousExporter
	})

	requests := []artworkRequest{
		{playlistIndex: 1, trackIndex: 1, albumKey: "Library/Artist/With Art"},
		{playlistIndex: 1, trackIndex: 2, albumKey: "Library/Artist/With Art"},
		{playlistIndex: 1, trackIndex: 3, albumKey: "Library/Artist/No Art"},
	}
	exportCalls := 0
	artworkMusicExporter = func(_ []Playlist, temporaryDir string, pending []artworkRequest) error {
		exportCalls++
		if len(pending) != 3 {
			t.Fatalf("pending requests=%#v", pending)
		}
		return os.WriteFile(filepath.Join(temporaryDir, "1-2.jpg"), []byte("image"), 0600)
	}

	first := []Playlist{{Name: "P", Tracks: []Track{{}, {}, {}}}}
	firstTemporaryDir := t.TempDir()
	if err := exportArtworks(first, firstTemporaryDir, requests); err != nil {
		t.Fatal(err)
	}
	cachedImage := first[0].Tracks[0].Artwork
	if cachedImage == "" || first[0].Tracks[1].Artwork != cachedImage {
		t.Fatalf("cached artwork was not applied to the album: %#v", first[0].Tracks)
	}
	if data, err := os.ReadFile(cachedImage); err != nil || string(data) != "image" {
		t.Fatalf("cached image=%q, err=%v", data, err)
	}
	if _, state := lookupArtworkCache(cacheRoot, "Library/Artist/No Art"); state != artworkMissing {
		t.Fatalf("missing artwork state=%v", state)
	}
	if err := os.RemoveAll(firstTemporaryDir); err != nil {
		t.Fatal(err)
	}

	artworkMusicExporter = func([]Playlist, string, []artworkRequest) error {
		t.Fatal("Music.app exporter was called for cached albums")
		return nil
	}
	second := []Playlist{{Name: "P", Tracks: []Track{{}, {}, {}}}}
	if err := exportArtworks(second, t.TempDir(), requests); err != nil {
		t.Fatal(err)
	}
	if second[0].Tracks[0].Artwork != cachedImage || second[0].Tracks[2].Artwork != "" {
		t.Fatalf("cache was not reused: %#v", second[0].Tracks)
	}
	if exportCalls != 1 {
		t.Fatalf("export calls=%d", exportCalls)
	}
}

func TestExportArtworksDoesNotCacheFailedMusicRequest(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "artwork")
	previousLocation := artworkCacheLocation
	previousExporter := artworkMusicExporter
	artworkCacheLocation = func() (string, error) { return cacheRoot, nil }
	artworkMusicExporter = func([]Playlist, string, []artworkRequest) error {
		return errors.New("Music.app disconnected")
	}
	t.Cleanup(func() {
		artworkCacheLocation = previousLocation
		artworkMusicExporter = previousExporter
	})

	request := artworkRequest{playlistIndex: 1, trackIndex: 1, albumKey: "Library/A/Album"}
	err := exportArtworks([]Playlist{{Name: "P", Tracks: []Track{{}}}}, t.TempDir(), []artworkRequest{request})
	if err == nil {
		t.Fatal("failed export returned nil")
	}
	if _, state := lookupArtworkCache(cacheRoot, request.albumKey); state != artworkNotCached {
		t.Fatalf("failed request was cached with state %v", state)
	}
}

func TestExportArtworksReturnsCacheLocationFailure(t *testing.T) {
	previousLocation := artworkCacheLocation
	artworkCacheLocation = func() (string, error) {
		return "", errors.New("cache directory unavailable")
	}
	t.Cleanup(func() { artworkCacheLocation = previousLocation })

	request := artworkRequest{playlistIndex: 1, trackIndex: 1, albumKey: "Library/A/Album"}
	err := exportArtworks(
		[]Playlist{{Name: "P", Tracks: []Track{{}}}},
		t.TempDir(),
		[]artworkRequest{request},
	)
	if err == nil || !strings.Contains(err.Error(), "cache directory unavailable") {
		t.Fatalf("error=%v", err)
	}
}

func TestArtworkCacheUsesAlbumKeyAndShardedPath(t *testing.T) {
	root := t.TempDir()
	key := "Library/Artist/Album"
	source := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(source, []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	cached, err := storeArtworkImage(root, key, source)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(cached)) != root {
		t.Fatalf("cache path is not sharded below root: %s", cached)
	}
	if got, state := lookupArtworkCache(root, key); state != artworkCached || got != cached {
		t.Fatalf("lookup=(%q,%v), want (%q,%v)", got, state, cached, artworkCached)
	}
}

func TestArtworkCacheImageReplacesMissingMarkerAndCannotBeDowngraded(t *testing.T) {
	root := t.TempDir()
	key := "Library/Artist/Album"
	if err := storeMissingArtwork(root, key); err != nil {
		t.Fatal(err)
	}
	if _, state := lookupArtworkCache(root, key); state != artworkMissing {
		t.Fatalf("initial state=%v", state)
	}

	source := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(source, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	image, err := storeArtworkImage(root, key, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artworkCacheKeyPath(root, key, ".none")); !os.IsNotExist(err) {
		t.Fatalf("missing marker remains after image: %v", err)
	}
	if err := storeMissingArtwork(root, key); err != nil {
		t.Fatal(err)
	}
	if got, state := lookupArtworkCache(root, key); state != artworkCached || got != image {
		t.Fatalf("image was downgraded: path=%q state=%v", got, state)
	}
	if cached, err := storeArtworkImage(root, key, filepath.Join(t.TempDir(), "does-not-exist")); err != nil || cached != image {
		t.Fatalf("existing image was not reused: path=%q err=%v", cached, err)
	}
}

func TestArtworkCachePathUsesUserCacheDirectory(t *testing.T) {
	path, err := artworkCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "artwork" ||
		filepath.Base(filepath.Dir(path)) != "Music Bridge" {
		t.Fatalf("artwork cache path=%q", path)
	}
}
