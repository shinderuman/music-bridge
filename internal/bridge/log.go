package bridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

var diagnosticLog struct {
	sync.Mutex
	file      *os.File
	path      string
	dir       string
	timestamp string
}

var diagnosticLogHome = os.UserHomeDir

func startDiagnosticLog() (func(), error) {
	home, err := diagnosticLogHome()
	if err != nil {
		return nil, err
	}
	return startDiagnosticLogIn(home)
}

func startDiagnosticLogIn(home string) (func(), error) {
	dir := filepath.Join(home, "Library", "Logs", "Music Bridge")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	timestamp := time.Now().Format("20060102-150405")
	path := filepath.Join(dir, "music-bridge-starting-"+timestamp+".log")
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	diagnosticLog.Lock()
	diagnosticLog.file = file
	diagnosticLog.path = path
	diagnosticLog.dir = dir
	diagnosticLog.timestamp = timestamp
	diagnosticLog.Unlock()
	logf("music-bridge started")
	return func() {
		diagnosticLog.Lock()
		defer diagnosticLog.Unlock()
		if diagnosticLog.file != nil {
			_ = diagnosticLog.file.Close()
			diagnosticLog.file = nil
		}
	}, nil
}

func setDiagnosticLogContext(context string) error {
	safeContext := sanitizeLogContext(context)
	if safeContext == "" {
		return nil
	}
	diagnosticLog.Lock()
	defer diagnosticLog.Unlock()
	if diagnosticLog.file == nil || diagnosticLog.path == "" {
		return nil
	}
	nextPath := filepath.Join(
		diagnosticLog.dir,
		"music-bridge-"+safeContext+"-"+diagnosticLog.timestamp+".log",
	)
	if nextPath == diagnosticLog.path {
		return nil
	}
	if err := os.Rename(diagnosticLog.path, nextPath); err != nil {
		return err
	}
	diagnosticLog.path = nextPath
	return nil
}

func sanitizeLogContext(value string) string {
	var result strings.Builder
	pendingSeparator := false
	for _, character := range strings.TrimSpace(value) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || character == '_' {
			if pendingSeparator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(character)
			pendingSeparator = false
		} else {
			pendingSeparator = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func diagnosticWriter() io.Writer {
	diagnosticLog.Lock()
	defer diagnosticLog.Unlock()
	if diagnosticLog.file == nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, diagnosticLog.file)
}

func diagnosticLogPath() string {
	diagnosticLog.Lock()
	defer diagnosticLog.Unlock()
	return diagnosticLog.path
}

func logf(format string, args ...any) {
	diagnosticLog.Lock()
	defer diagnosticLog.Unlock()
	if diagnosticLog.file == nil {
		return
	}
	fmt.Fprintf(diagnosticLog.file, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}
