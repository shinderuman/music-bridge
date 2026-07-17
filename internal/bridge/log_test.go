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
