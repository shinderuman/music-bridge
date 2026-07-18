package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogfWritesDiagnosticMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostic.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticLog.Lock()
	previousFile, previousPath := diagnosticLog.file, diagnosticLog.path
	diagnosticLog.file, diagnosticLog.path = file, path
	diagnosticLog.Unlock()
	t.Cleanup(func() {
		diagnosticLog.Lock()
		diagnosticLog.file, diagnosticLog.path = previousFile, previousPath
		diagnosticLog.Unlock()
		_ = file.Close()
	})

	logf("rsync failed: %d", 23)
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "rsync failed: 23") {
		t.Fatalf("diagnostic log = %q", data)
	}
}

func TestStartDiagnosticLogWritesAndClosesFile(t *testing.T) {
	closeLog, err := startDiagnosticLogIn(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := diagnosticLogPath()
	defer func() {
		closeLog()
		diagnosticLog.Lock()
		diagnosticLog.path = ""
		diagnosticLog.Unlock()
	}()

	if path == "" {
		t.Fatal("diagnostic log path is empty")
	}
	if _, err := diagnosticWriter().Write([]byte("rsync stderr\n")); err != nil {
		t.Fatal(err)
	}
	closeLog()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "music-bridge started") || !strings.Contains(string(data), "rsync stderr") {
		t.Fatalf("diagnostic log = %q", data)
	}
}

func TestDiagnosticLogContextRenamesFileAndKeepsWriting(t *testing.T) {
	closeLog, err := startDiagnosticLogIn(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()
	original := diagnosticLogPath()
	if err := setDiagnosticLogContext("android-Pixel 6a-内部ストレージ"); err != nil {
		t.Fatal(err)
	}
	renamed := diagnosticLogPath()
	if renamed == original {
		t.Fatal("diagnostic log was not renamed")
	}
	if got := filepath.Base(renamed); !strings.HasPrefix(got, "music-bridge-android-Pixel-6a-内部ストレージ-") {
		t.Fatalf("renamed log=%q", got)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("original log still exists: %v", err)
	}
	logf("after rename")
	if err := diagnosticLog.file.Sync(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "after rename") {
		t.Fatalf("renamed log=%q", data)
	}
}

func TestSanitizeLogContext(t *testing.T) {
	if got := sanitizeLogContext("drive-/Volumes/SDXC 128GB"); got != "drive-Volumes-SDXC-128GB" {
		t.Fatalf("sanitized context=%q", got)
	}
}

func TestRunLogsError(t *testing.T) {
	previousHome := diagnosticLogHome
	diagnosticLogHome = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { diagnosticLogHome = previousHome })

	err := Run([]string{"sync"})
	if err == nil {
		t.Fatal("Run(sync) succeeded, want removed-subcommand error")
	}
	data, readErr := os.ReadFile(diagnosticLogPath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "error: サブコマンドは廃止されました") {
		t.Fatalf("diagnostic log = %q", data)
	}
	if got, want := DiagnosticLogPath(), diagnosticLogPath(); got != want {
		t.Fatalf("DiagnosticLogPath = %q, want %q", got, want)
	}
}
