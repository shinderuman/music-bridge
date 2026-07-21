package drive

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPlanHelpersAndExistingBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.m4a")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	playlists := []Playlist{{Tracks: []Track{{Name: "one", Artist: "Artist", Album: "Album", Location: source}, {Name: "duplicate", Artist: "Artist", Album: "Album", Location: source}, {Name: "missing"}}}}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || !reflect.DeepEqual(missing, []string{"missing"}) || countTracks(playlists) != 3 || totalBytes(plan) != 4 {
		t.Fatalf("plan=%#v missing=%#v", plan, missing)
	}
	if got := makeAudioTransferPlan(plan, root); got.bytes != 4 || len(got.items) != 1 {
		t.Fatalf("audio transfer plan=%#v", got)
	}
	destination := filepath.Join(root, plan[0].Relative)
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(source, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(destination, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got := makeAudioTransferPlan(plan, root); got.bytes != 0 || len(got.items) != 0 {
		t.Fatalf("audio transfer plan with existing file=%#v", got)
	}
	if free, err := freeBytes(root); err != nil || free <= 0 {
		t.Fatalf("freeBytes=%d,%v", free, err)
	}
}

func TestMakePlanSkipsNonRegularLocationsAndFitPlanHonorsBudget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.m4a")
	second := filepath.Join(root, "second.m4a")
	if err := os.WriteFile(first, []byte("1111"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("2222"), 0644); err != nil {
		t.Fatal(err)
	}
	playlists := []Playlist{{Tracks: []Track{{Name: "directory", Location: root}, {Name: "first", Artist: "A", Album: "X", Location: first}, {Name: "second", Artist: "A", Album: "X", Location: second}}}}
	plan, missing, err := makePlan(playlists)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []string{"directory"}) || len(plan) != 2 {
		t.Fatalf("plan=%#v missing=%#v", plan, missing)
	}
	if got := fitPlan(plan, filepath.Join(root, "target"), 4); !reflect.DeepEqual(got, plan[:1]) {
		t.Fatalf("fitPlan=%#v", got)
	}
}

func TestPlaylistAndTargetFileHelpers(t *testing.T) {
	root := t.TempDir()
	plan := []Planned{{Track: Track{Location: "/source/song.m4a"}, Relative: "Library/A/Album/song.m4a"}}
	playlists := []Playlist{{Name: "Keep", Tracks: []Track{{Location: "/source/song.m4a"}}}, {Name: "Remove"}}
	playlistPlan, err := makePlaylistSyncPlan(playlists, plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := playlistPlan.write(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DeletedFromMusic.m3u"), []byte("#EXTM3U\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "Keep.m3u"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "\xef\xbb\xbf#EXTM3U\nLibrary/A/Album/song.m4a\n"; got != want {
		t.Fatalf("playlist=%q", got)
	}
	playlistPlan, err = makePlaylistSyncPlan(playlists[:1], plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := playlistTargetPaths(playlistPlan.stale), []string{
		filepath.Join(root, "DeletedFromMusic.m3u"),
		filepath.Join(root, "Remove.m3u"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale=%#v", got)
	}
	playlistPlan, err = makePlaylistSyncPlan([]Playlist{{Name: "keep"}}, plan, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := playlistTargetPaths(playlistPlan.stale), []string{
		filepath.Join(root, "DeletedFromMusic.m3u"),
		filepath.Join(root, "Remove.m3u"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("case-insensitive stale=%#v", got)
	}
	if err := playlistPlan.removeStale(false); err != nil {
		t.Fatal(err)
	}
	if got, want := string(prefixLibraryInM3U([]byte("#EXTM3U\r\nA/B.m4a\r\nLibrary/C.m4a\r\n"))), "#EXTM3U\r\nLibrary/A/B.m4a\r\nLibrary/C.m4a\r\n"; got != want {
		t.Fatalf("prefixed=%q", got)
	}
	if err := saveManifest(root, []Planned{{Relative: "B"}, {Relative: "A"}}); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest(root); !reflect.DeepEqual(got, []string{"A", "B", "Keep.m3u"}) {
		t.Fatalf("manifest=%#v", got)
	}
	empty := filepath.Join(root, "empty", "nested")
	keep := filepath.Join(root, "keep")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyDirs(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Fatalf("empty dir remains: %v", err)
	}
}

func TestCopyStageAndBatchHelpers(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.m4a")
	destination := filepath.Join(dir, "destination.m4a")
	if err := os.WriteFile(source, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := copyFile(source, destination, stamp); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "song" {
		t.Fatalf("copy=%q,%v", data, err)
	}
	item := Planned{Track: Track{Location: source}, Relative: "Library/A/song.m4a", Size: 4}
	stage, err := stagePlan([]Planned{item})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if target, err := os.Readlink(filepath.Join(stage, item.Relative)); err != nil || target != source {
		t.Fatalf("stage link=%q,%v", target, err)
	}
	if batchBytes([]Planned{item, {Size: 3}}) != 7 {
		t.Fatal("batchBytes")
	}
}

func TestFilesystemHelpersHandleDryRunAndInvalidManifest(t *testing.T) {
	root := t.TempDir()
	plan := []Planned{{Track: Track{Location: "/source/song.m4a"}, Relative: "Library/A/Album/song.m4a"}}
	playlistPlan, err := makePlaylistSyncPlan(
		[]Playlist{{Name: "P", Tracks: []Track{{Location: "/source/song.m4a"}}}},
		plan, root,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := playlistPlan.write(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "P.m3u")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote playlist: %v", err)
	}
	artworkPlan, err := makeArtworkTransferPlan(plan, root, map[string]bool{"Library/A/Album": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := artworkPlan.write(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Library")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created artwork directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest(root); got != nil {
		t.Fatalf("invalid manifest = %#v, want nil", got)
	}
}

func playlistTargetPaths(files []playlistTargetFile) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.path)
	}
	return result
}
