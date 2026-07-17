package bridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var diagnosticLog struct {
	sync.Mutex
	file *os.File
	path string
}

func startDiagnosticLog() (func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, "Library", "Logs", "Music Bridge")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "music-bridge-"+time.Now().Format("20060102-150405")+".log")
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	diagnosticLog.Lock()
	diagnosticLog.file = file
	diagnosticLog.path = path
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
