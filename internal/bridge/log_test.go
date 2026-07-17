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
