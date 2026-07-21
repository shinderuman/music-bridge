package diagnostic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerStartsWritesRenamesAndCloses(t *testing.T) {
	logger := NewAtHome(t.TempDir())
	closeLog, err := logger.Start()
	if err != nil {
		t.Fatal(err)
	}
	original := logger.Path()
	if original == "" {
		t.Fatal("diagnostic log path is empty")
	}
	if _, err := logger.Writer().Write([]byte("rsync stderr\n")); err != nil {
		t.Fatal(err)
	}
	logger.Printf("rsync failed: %d", 23)
	if err := logger.SetContext("android-Pixel 6a-内部ストレージ"); err != nil {
		t.Fatal(err)
	}
	renamed := logger.Path()
	if renamed == original {
		t.Fatal("diagnostic log was not renamed")
	}
	if got := filepath.Base(renamed); !strings.HasPrefix(got, "music-bridge-android-Pixel-6a-内部ストレージ-") {
		t.Fatalf("renamed log=%q", got)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("original log still exists: %v", err)
	}
	if err := logger.Sync(); err != nil {
		t.Fatal(err)
	}
	closeLog()
	data, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"music-bridge started", "rsync stderr", "rsync failed: 23"} {
		if !strings.Contains(string(data), message) {
			t.Fatalf("diagnostic log %q does not contain %q", data, message)
		}
	}
}

func TestSanitizeContext(t *testing.T) {
	if got := SanitizeContext("drive-/Volumes/SDXC 128GB"); got != "drive-Volumes-SDXC-128GB" {
		t.Fatalf("sanitized context=%q", got)
	}
}
