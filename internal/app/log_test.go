package app

import (
	"os"
	"strings"
	"testing"

	"music-bridge/internal/diagnostic"
)

func TestRunLogsError(t *testing.T) {
	previous := diagnosticLog
	diagnosticLog = diagnostic.NewAtHome(t.TempDir())
	t.Cleanup(func() { diagnosticLog = previous })

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
