package android

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAndroidBackend struct {
	files        map[string][]byte
	appendCalls  int
	failFirstAt  int
	finalized    string
	finalizedRel string
	signatures   map[string]string
}

func (fake *fakeAndroidBackend) PartialState(path string) (int64, string, error) {
	data, ok := fake.files[path]
	if !ok {
		return -1, fake.signatures[path], nil
	}
	return int64(len(data)), fake.signatures[path], nil
}

func (fake *fakeAndroidBackend) PreparePartial(path, signature string) error {
	if fake.signatures == nil {
		fake.signatures = map[string]string{}
	}
	fake.signatures[path] = signature
	return nil
}

func (fake *fakeAndroidBackend) ResetPartial(path string) error {
	delete(fake.files, path)
	delete(fake.signatures, path)
	return nil
}

func (*fakeAndroidBackend) MakeDir(string) error { return nil }

func (fake *fakeAndroidBackend) Append(path string, input io.Reader) error {
	fake.appendCalls++
	if fake.appendCalls == 1 && fake.failFirstAt > 0 {
		buffer := make([]byte, fake.failFirstAt)
		count, _ := io.ReadFull(input, buffer)
		fake.files[path] = append(fake.files[path], buffer[:count]...)
		return errors.New("device offline")
	}
	data, err := io.ReadAll(input)
	fake.files[path] = append(fake.files[path], data...)
	return err
}

func (fake *fakeAndroidBackend) Finalize(partial, destination string, _ int64, relative string) error {
	fake.files[destination] = append([]byte(nil), fake.files[partial]...)
	delete(fake.files, partial)
	delete(fake.signatures, partial)
	fake.finalized = destination
	fake.finalizedRel = relative
	return nil
}

func (fake *fakeAndroidBackend) Remove(path string) error {
	delete(fake.files, path)
	return nil
}

func TestCopyAndroidFileResumesAtRemotePartialSize(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.m4a")
	content := bytes.Repeat([]byte("abcdef"), 10)
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	previousChunkSize := androidChunkSize
	androidChunkSize = 16
	t.Cleanup(func() { androidChunkSize = previousChunkSize })

	fake := &fakeAndroidBackend{files: map[string][]byte{}, signatures: map[string]string{}, failFirstAt: 7}
	waits := 0
	err := copyAndroidFile(context.Background(), fake, source, "/sd/music-bridge/song.m4a", "Library/song.m4a",
		func(context.Context, error) error { waits++; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.files["/sd/music-bridge/song.m4a"]; !bytes.Equal(got, content) {
		t.Fatalf("copied content differs: %q", got)
	}
	if waits != 1 || fake.appendCalls < 2 {
		t.Fatalf("waits=%d appendCalls=%d", waits, fake.appendCalls)
	}
	if fake.finalizedRel != "Library/song.m4a" {
		t.Fatalf("managed relative=%q", fake.finalizedRel)
	}
}

func TestCopyAndroidFileDropsOversizedPartial(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("small"), 0644); err != nil {
		t.Fatal(err)
	}
	partial := "/target" + androidPartialSuffix
	fake := &fakeAndroidBackend{files: map[string][]byte{partial: []byte("too large")}, signatures: map[string]string{}}
	if err := copyAndroidFile(context.Background(), fake, source, "/target", "target",
		func(context.Context, error) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(fake.files["/target"]); got != "small" {
		t.Fatalf("content=%q", got)
	}
}

func TestCopyAndroidFileDropsPartialWithDifferentSourceSignature(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new-data"), 0644); err != nil {
		t.Fatal(err)
	}
	partial := "/target" + androidPartialSuffix
	fake := &fakeAndroidBackend{
		files:      map[string][]byte{partial: []byte("old-")},
		signatures: map[string]string{partial: "8:1"},
	}
	if err := copyAndroidFile(context.Background(), fake, source, "/target", "target",
		func(context.Context, error) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(fake.files["/target"]); got != "new-data" {
		t.Fatalf("content=%q", got)
	}
}

func TestRetryAndroidOperationStopsForPermanentErrorAndCancellation(t *testing.T) {
	permanent := errors.New("permission denied")
	if got := retryAndroidOperation(context.Background(), func() error { return permanent },
		func(context.Context, error) error { t.Fatal("wait called"); return nil }); !errors.Is(got, permanent) {
		t.Fatalf("error=%v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retryAndroidOperation(ctx, func() error {
		calls++
		return errors.New("device offline")
	}, func(ctx context.Context, _ error) error {
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestIsRetryableADBErrorAcceptsTransportExit255(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 255").Run()
	if !isRetryableADBError(err) {
		t.Fatal("adb exit 255 was not treated as retryable")
	}
}

func TestIsRetryableADBErrorAcceptsNamedDeviceNotFound(t *testing.T) {
	err := errors.New("adb: device 'adb-example._adb-tls-connect._tcp' not found")
	if !isRetryableADBError(err) {
		t.Fatal("named adb device not found was not treated as retryable")
	}
}

func TestWaitForAndroidHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForAndroid("device:5555")(ctx, errors.New("device offline"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestAndroidConnectionMonitorProbesDuringLongLocalOperation(t *testing.T) {
	previousInterval := androidConnectionCheckInterval
	androidConnectionCheckInterval = time.Millisecond
	t.Cleanup(func() { androidConnectionCheckInterval = previousInterval })

	var mutex sync.Mutex
	var commands [][]string
	probed := make(chan struct{}, 1)
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		mutex.Lock()
		commands = append(commands, append([]string(nil), args...))
		mutex.Unlock()
		select {
		case probed <- struct{}{}:
		default:
		}
		return nil, nil
	}})

	err := withAndroidConnectionMonitor(
		context.Background(),
		fixedAndroidSerial("device:5555"),
		func(context.Context, error) error {
			t.Fatal("reconnect wait was called")
			return nil
		},
		func() error {
			select {
			case <-probed:
				return nil
			case <-time.After(time.Second):
				t.Fatal("connection probe was not sent")
				return nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(commands) == 0 || !reflect.DeepEqual(commands[0], []string{"-s", "device:5555", "shell", "-T", ":"}) {
		t.Fatalf("probe commands=%#v", commands)
	}
}

func TestAndroidConnectionMonitorReconnectsAfterProbeFailure(t *testing.T) {
	previousInterval := androidConnectionCheckInterval
	androidConnectionCheckInterval = time.Millisecond
	t.Cleanup(func() { androidConnectionCheckInterval = previousInterval })

	var mutex sync.Mutex
	probes := 0
	reconnected := make(chan struct{})
	var reconnectOnce sync.Once
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		mutex.Lock()
		probes++
		current := probes
		mutex.Unlock()
		if current == 1 {
			return []byte("adb: device 'device:5555' not found"), errors.New("exit status 1")
		}
		reconnectOnce.Do(func() { close(reconnected) })
		return nil, nil
	}})

	waits := 0
	err := withAndroidConnectionMonitor(
		context.Background(),
		fixedAndroidSerial("device:5555"),
		func(context.Context, error) error {
			waits++
			return nil
		},
		func() error {
			select {
			case <-reconnected:
				return nil
			case <-time.After(time.Second):
				t.Fatal("connection monitor did not recover")
				return nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if waits != 1 || probes < 2 {
		t.Fatalf("waits=%d probes=%d", waits, probes)
	}
}

func TestParseAndroidInventory(t *testing.T) {
	root := "/storage/ABCD/music-bridge"
	data := root + "/Library/A/a|b.m4a\x00123\x001700000000.750000000\x00invalid\x00x\x00y\x00"
	want := map[string]androidFileState{"Library/A/a|b.m4a": {Size: 123, ModTime: 1700000000}}
	if got := parseAndroidInventory(data, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory=%#v", got)
	}
}

func TestAndroidInventoryCommandDoesNotUseUnboundedExecArguments(t *testing.T) {
	command := androidInventoryCommand("/storage/SD Card/music-bridge")
	for _, fragment := range []string{
		"find '/storage/SD Card/music-bridge'",
		"-type f -printf '%p\\0%s\\0%T@\\0'",
	} {
		if !strings.Contains(command, fragment) {
			t.Fatalf("inventory command %q does not contain %q", command, fragment)
		}
	}
	if strings.Contains(command, "-exec") {
		t.Fatalf("inventory command still uses find -exec: %q", command)
	}
}

func TestAndroidPlaylistHashCommandAndParser(t *testing.T) {
	root := "/storage/SD Card/music-bridge"
	command := androidPlaylistHashCommand(root)
	for _, fragment := range []string{
		"for file in '/storage/SD Card/music-bridge'/*.m3u",
		"sha256sum \"$file\"",
		"printf '%s\\0%s\\0'",
	} {
		if !strings.Contains(command, fragment) {
			t.Fatalf("playlist hash command %q does not contain %q", command, fragment)
		}
	}
	hash := strings.Repeat("a", sha256.Size*2)
	data := root + "/P 1.m3u\x00" + hash + "\x00invalid\x00short\x00"
	want := map[string]string{"P 1.m3u": hash}
	if got := parseAndroidPlaylistHashes(data, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("playlist hashes=%#v, want %#v", got, want)
	}
}

func TestAndroidInventoryMergesPlaylistContentHash(t *testing.T) {
	root := "/storage/SD/music-bridge"
	hash := strings.Repeat("b", sha256.Size*2)
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		command := args[len(args)-1]
		switch {
		case strings.Contains(command, "-type f -printf"):
			return []byte(root + "/P.m3u\x0010\x001700000000\x00"), nil
		case strings.Contains(command, "sha256sum"):
			return []byte(root + "/P.m3u\x00" + hash + "\x00"), nil
		default:
			t.Fatalf("unexpected command: %q", command)
			return nil, nil
		}
	}})
	inventory, err := androidInventory("device:5555", root)
	if err != nil {
		t.Fatal(err)
	}
	if got := inventory["P.m3u"]; got != (androidFileState{Size: 10, ModTime: 1700000000, Hash: hash}) {
		t.Fatalf("playlist state=%#v", got)
	}
}

func TestAndroidInventoryWithProgressStreamsFileCount(t *testing.T) {
	root := "/storage/SD/music-bridge"
	data := root + "/Library/one.m4a\x004\x001700000000.5\x00" +
		root + "/Library/two.m4a\x005\x001700000001.5\x00"
	useFakeADB(t, fakeStreamingADBExecutor{
		fakeADBExecutor: fakeADBExecutor{output: func(args ...string) ([]byte, error) {
			if strings.Contains(args[len(args)-1], "sha256sum") {
				return nil, nil
			}
			t.Fatalf("unexpected buffered adb command: %q", args)
			return nil, nil
		}},
		stream: func(args ...string) (io.ReadCloser, func() ([]byte, error), error) {
			if !strings.Contains(args[len(args)-1], "-type f -printf") {
				t.Fatalf("unexpected streaming adb command: %q", args)
			}
			return io.NopCloser(strings.NewReader(data)), func() ([]byte, error) {
				return nil, nil
			}, nil
		},
	})
	var counts []int
	inventory, count, err := androidInventoryWithProgress("device:5555", root, func(count int) {
		counts = append(counts, count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || !reflect.DeepEqual(counts, []int{1, 2}) {
		t.Fatalf("count=%d, progress=%#v", count, counts)
	}
	if got := inventory["Library/two.m4a"]; got != (androidFileState{Size: 5, ModTime: 1700000001}) {
		t.Fatalf("second inventory entry=%#v", got)
	}
}

func TestParseAndroidInventoryStreamCountsMalformedRecords(t *testing.T) {
	root := "/storage/SD/music-bridge"
	data := root + "/Library/good.m4a\x004\x001700000000\x00" +
		root + "/Library/malformed.m4a\x00bad\x001700000001\x00"
	var counts []int
	inventory, count, err := parseAndroidInventoryStream(strings.NewReader(data), root, func(count int) {
		counts = append(counts, count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(inventory) != 1 || !reflect.DeepEqual(counts, []int{1, 2}) {
		t.Fatalf("inventory=%#v, count=%d, progress=%#v", inventory, count, counts)
	}
}

func TestAndroidInventoryReturnsPlaylistHashFailure(t *testing.T) {
	root := "/storage/SD/music-bridge"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		command := args[len(args)-1]
		if strings.Contains(command, "-type f -printf") {
			return nil, nil
		}
		return []byte("sha256sum: not found"), errors.New("exit status 127")
	}})
	_, err := androidInventory("device:5555", root)
	if err == nil || !strings.Contains(err.Error(), "プレイリスト") ||
		!strings.Contains(err.Error(), "sha256sum: not found") {
		t.Fatalf("error=%v", err)
	}
}

func TestSameAndroidFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "song")
	if err := os.WriteFile(file, []byte("song"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if !sameAndroidFile(file, androidFileState{Size: 4, ModTime: stamp.Unix() + 2}) {
		t.Fatal("same file not recognized")
	}
	if sameAndroidFile(file, androidFileState{Size: 5, ModTime: stamp.Unix()}) {
		t.Fatal("different size recognized")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("song")))
	if !sameAndroidFile(file, androidFileState{Size: 4, ModTime: 0, Hash: hash}) {
		t.Fatal("matching content hash not recognized")
	}
	differentHash := fmt.Sprintf("%x", sha256.Sum256([]byte("long")))
	if sameAndroidFile(file, androidFileState{Size: 4, ModTime: stamp.Unix(), Hash: differentHash}) {
		t.Fatal("different same-size content hash recognized")
	}
	if got := androidFileDifference(file, androidFileState{}, false); got != "Android上に存在しません" {
		t.Fatalf("missing reason=%q", got)
	}
	if got := androidFileDifference(file, androidFileState{Size: 4, ModTime: stamp.Unix() + 10}, true); !strings.Contains(got, "更新時刻不一致") {
		t.Fatalf("mtime reason=%q", got)
	}
}

func TestAndroidStalePaths(t *testing.T) {
	got := androidStalePaths([]string{"B", "A", "Keep"}, map[string]bool{"Keep": true})
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("stale=%#v", got)
	}
}

func TestParseAndroidManagedPathsMergesManifestAndInterruptedJournal(t *testing.T) {
	data := "[\n  \"Library/A.m4a\",\n  \"Library/B.m4a\"\n]\nLibrary/C.m4a\nLibrary/A.m4a\n"
	got := parseAndroidManagedPaths(data)
	want := []string{"Library/A.m4a", "Library/B.m4a", "Library/C.m4a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managed=%#v, want %#v", got, want)
	}
}

func TestLoadAndroidManagedPathsNormalizesMacManifestPaths(t *testing.T) {
	root := "/storage/SD/music-bridge"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		return []byte("[\n  \"Library/Artist:Name/Album \\\"BEST\\\"/song.m4a\"\n]\n" +
			"Library/Artist\uf022Name/Album \uf020BEST\uf020/song.m4a\n"), nil
	}})
	got, err := loadAndroidManagedPaths("device:5555", root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Library/Artist\uf022Name/Album \uf020BEST\uf020/song.m4a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managed paths=%#v, want %#v", got, want)
	}
}

func TestMissingAndroidManifestCommandEndsSuccessfully(t *testing.T) {
	root := "/storage/ABCD/music-bridge"
	command := "for f in " + shellQuote(path.Join(root, manifest)) + " " +
		shellQuote(path.Join(root, androidPendingManifest)) +
		"; do [ -f \"$f\" ] && cat \"$f\"; done; true"
	if !strings.HasSuffix(command, "; true") {
		t.Fatalf("command can fail when both manifests are absent: %q", command)
	}
}

func TestSaveAndroidManifestMarksArtworkAsIndexed(t *testing.T) {
	root := "/storage/SD/music-bridge"
	var commands []string
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			commands = append(commands, strings.Join(args, " "))
			return nil, nil
		},
		run: func(_ io.Reader, args ...string) ([]byte, error) {
			commands = append(commands, strings.Join(args, " "))
			return nil, nil
		},
	})
	if err := saveAndroidManifest("device:5555", root, []string{"Library/song.m4a"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, libraryManifestMarker) {
		t.Fatalf("library index marker was not written:\n%s", joined)
	}
}

func TestSaveAndroidPendingPathsPreservesManagedAndPlannedContent(t *testing.T) {
	root := "/storage/SD/music-bridge"
	var written []byte
	useFakeADB(t, fakeADBExecutor{
		run: func(input io.Reader, args ...string) ([]byte, error) {
			if !strings.Contains(args[len(args)-1], androidPendingManifest) {
				t.Fatalf("unexpected destination command: %q", args[len(args)-1])
			}
			var err error
			written, err = io.ReadAll(input)
			return nil, err
		},
	})
	desired := map[string]bool{
		"Library/new.m4a":                   true,
		"Library/Artist/Album/AlbumArt.jpg": true,
	}
	if err := saveAndroidPendingPaths(
		"device:5555", root, []string{"Library/old.m4a", "Library/new.m4a"}, desired,
	); err != nil {
		t.Fatal(err)
	}
	want := "Library/Artist/Album/AlbumArt.jpg\nLibrary/new.m4a\nLibrary/old.m4a\n"
	if got := string(written); got != want {
		t.Fatalf("pending manifest=%q, want %q", got, want)
	}
}

func TestADBAndroidBackendCommands(t *testing.T) {
	var shellCommands []string
	var written []byte
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			command := args[len(args)-1]
			shellCommands = append(shellCommands, command)
			if strings.Contains(command, "stat -c %s") {
				return []byte("3\n3:1700000000\n"), nil
			}
			return nil, nil
		},
		run: func(input io.Reader, args ...string) ([]byte, error) {
			written, _ = io.ReadAll(input)
			return nil, nil
		},
	})
	backend := adbAndroidBackend{serial: fixedAndroidSerial("device:5555"), root: "/storage/SD/music-bridge"}
	size, signature, err := backend.PartialState("/storage/SD/part")
	if err != nil || size != 3 || signature != "3:1700000000" {
		t.Fatalf("partial=%d,%q,%v", size, signature, err)
	}
	if err := backend.PreparePartial("/storage/SD/part", "3:1700000000"); err != nil {
		t.Fatal(err)
	}
	if string(written) != "3:1700000000\n" {
		t.Fatalf("metadata=%q", written)
	}
	for _, operation := range []func() error{
		func() error { return backend.MakeDir("/storage/SD/dir") },
		func() error { return backend.ResetPartial("/storage/SD/part") },
		func() error { return backend.Append("/storage/SD/part", strings.NewReader("abc")) },
		func() error {
			return backend.Finalize("/storage/SD/part", "/storage/SD/final", 1700000000, "Library/final")
		},
		func() error { return backend.Remove("/storage/SD/final") },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	if len(shellCommands) < 5 {
		t.Fatalf("shell commands=%#v", shellCommands)
	}
}

func TestAndroidRemoteInspectionAndManifestCommands(t *testing.T) {
	root := "/storage/SD/music-bridge"
	var writes int
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			command := args[len(args)-1]
			switch {
			case strings.Contains(command, "-type f -printf"):
				return []byte(root + "/Library/song\x004\x001700000000\x00"), nil
			case strings.HasPrefix(command, "df -k"):
				return []byte("/dev/test 100 25 75 25% /storage/SD\n"), nil
			case strings.HasPrefix(command, "for f in"):
				return []byte("[\n \"Library/song\"\n]\nLibrary/interrupted\n"), nil
			default:
				return nil, nil
			}
		},
		run: func(input io.Reader, args ...string) ([]byte, error) {
			writes++
			_, _ = io.ReadAll(input)
			return nil, nil
		},
	})
	inventory, err := androidInventory("device:5555", root)
	if err != nil || inventory["Library/song"].Size != 4 {
		t.Fatalf("inventory=%#v, err=%v", inventory, err)
	}
	if free, err := androidFreeBytes("device:5555", "/storage/SD"); err != nil || free != 75*1024 {
		t.Fatalf("free=%d, err=%v", free, err)
	}
	managed, err := loadAndroidManagedPaths("device:5555", root)
	if err != nil || !reflect.DeepEqual(managed, []string{"Library/interrupted", "Library/song"}) {
		t.Fatalf("managed=%#v, err=%v", managed, err)
	}
	if err := saveAndroidManifest("device:5555", root, []string{"Library/song"}); err != nil {
		t.Fatal(err)
	}
	if err := removeAndroidEmptyDirs("device:5555", root); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("manifest writes=%d", writes)
	}
}
