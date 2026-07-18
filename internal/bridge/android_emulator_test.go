package bridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type delayedAndroidBackend struct {
	adbAndroidBackend
	delay      time.Duration
	onFirstRun func()
	once       sync.Once
}

func (backend *delayedAndroidBackend) Append(filePath string, input io.Reader) error {
	backend.once.Do(func() {
		if backend.onFirstRun != nil {
			backend.onFirstRun()
		}
	})
	err := backend.adbAndroidBackend.Append(filePath, slowReader{reader: input, delay: backend.delay})
	if err == nil {
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

type slowReader struct {
	reader io.Reader
	delay  time.Duration
}

func (reader slowReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 64*1024 {
		buffer = buffer[:64*1024]
	}
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		time.Sleep(reader.delay)
	}
	return count, err
}

func TestAndroidEmulatorExternalStorageIntegration(t *testing.T) {
	serial := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_SERIAL")
	volumePath := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_VOLUME")
	if serial == "" || volumePath == "" {
		t.Skip("Android emulator integration environment is not configured")
	}
	root := path.Join(volumePath, "music-bridge-integration-test")
	volumes, err := androidVolumes(serial)
	if err != nil {
		t.Fatal(err)
	}
	foundVolume := false
	for _, volume := range volumes {
		if volume.Path == volumePath && volume.Kind == "SDカード" {
			foundVolume = true
		}
	}
	if !foundVolume {
		t.Fatalf("virtual SD was not classified as SD card: %#v", volumes)
	}
	if out, err := adbShell(serial, "rm -rf "+shellQuote(root)+" && mkdir -p "+shellQuote(root)); err != nil {
		t.Fatalf("prepare: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_, _ = adbShell(serial, "rm -rf "+shellQuote(root))
	})

	source := filepath.Join(t.TempDir(), "resume-test.bin")
	content := bytes.Repeat([]byte("music-bridge-android-resume\n"), 64*1024)
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	backend := adbAndroidBackend{serial: fixedAndroidSerial(serial), root: root}
	destination := path.Join(root, "Library/Test/Resume/resume-test.bin")
	partial := destination + androidPartialSuffix
	if err := backend.MakeDir(path.Dir(destination)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	signature := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().Unix())
	if err := backend.PreparePartial(partial, signature); err != nil {
		t.Fatal(err)
	}
	seedSize := len(content) / 3
	if err := backend.Append(partial, bytes.NewReader(content[:seedSize])); err != nil {
		t.Fatal(err)
	}
	if err := copyAndroidFile(context.Background(), backend, source, destination,
		"Library/Test/Resume/resume-test.bin",
		func(context.Context, error) error { return fmt.Errorf("unexpected disconnect") },
		nil,
	); err != nil {
		t.Fatal(err)
	}
	inventory, err := androidInventory(serial, root)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := inventory["Library/Test/Resume/resume-test.bin"]
	if !ok || !sameAndroidFile(source, state) {
		t.Fatalf("remote state=%#v, exists=%v", state, ok)
	}
	out, err := adbShell(serial, "sha256sum "+shellQuote(destination))
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(content))
	if !strings.HasPrefix(string(out), wantHash) {
		t.Fatalf("remote hash=%q, want prefix %q", out, wantHash)
	}
	managed, err := loadAndroidManagedPaths(serial, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 1 || managed[0] != "Library/Test/Resume/resume-test.bin" {
		t.Fatalf("managed=%#v", managed)
	}
	if free, err := androidFreeBytes(serial, volumePath); err != nil || free <= 0 {
		t.Fatalf("free=%d, err=%v", free, err)
	}
}

func TestAndroidEmulatorResumesAfterTransportLoss(t *testing.T) {
	if os.Getenv("MUSIC_BRIDGE_ANDROID_DISCONNECT_TEST") != "1" {
		t.Skip("Android emulator disconnect test is not enabled")
	}
	serial := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_SERIAL")
	volumePath := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_VOLUME")
	if serial == "" || volumePath == "" {
		t.Fatal("Android emulator integration environment is not configured")
	}
	root := path.Join(volumePath, "music-bridge-disconnect-test")
	if out, err := adbShell(serial, "rm -rf "+shellQuote(root)+" && mkdir -p "+shellQuote(root)); err != nil {
		t.Fatalf("prepare: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_, _ = adbShell(serial, "rm -rf "+shellQuote(root))
	})

	content := bytes.Repeat([]byte("resume-after-real-transport-loss\n"), 1024*1024)
	source := filepath.Join(t.TempDir(), "disconnect.bin")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	previousChunkSize := androidChunkSize
	androidChunkSize = 1 << 20
	t.Cleanup(func() { androidChunkSize = previousChunkSize })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	backend := delayedAndroidBackend{
		adbAndroidBackend: adbAndroidBackend{serial: fixedAndroidSerial(serial), root: root},
		delay:             20 * time.Millisecond,
		onFirstRun: func() {
			go func() {
				time.Sleep(100 * time.Millisecond)
				_ = exec.Command("adb", "kill-server").Run()
			}()
		},
	}
	destination := path.Join(root, "Library/Test/Disconnect/disconnect.bin")
	reconnects := 0
	wait := waitForAndroid(serial)
	if err := copyAndroidFile(ctx, &backend, source, destination,
		"Library/Test/Disconnect/disconnect.bin", func(ctx context.Context, err error) error {
			reconnects++
			return wait(ctx, err)
		}, nil); err != nil {
		t.Fatal(err)
	}
	if reconnects == 0 {
		t.Fatal("ADB transport was not interrupted")
	}
	out, err := adbShell(serial, "sha256sum "+shellQuote(destination))
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(content))
	if !strings.HasPrefix(string(out), wantHash) {
		t.Fatalf("remote hash=%q, want prefix %q", out, wantHash)
	}
}
