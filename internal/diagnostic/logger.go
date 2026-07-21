package diagnostic

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

// Default is shared by the application and transport packages in one CLI process.
var Default = New(os.UserHomeDir)

type Logger struct {
	mu        sync.Mutex
	file      *os.File
	path      string
	dir       string
	timestamp string
	home      func() (string, error)
	stderr    io.Writer
}

func New(home func() (string, error)) *Logger {
	return &Logger{home: home, stderr: os.Stderr}
}

func NewAtHome(home string) *Logger {
	return New(func() (string, error) { return home, nil })
}

func (logger *Logger) Start() (func(), error) {
	home, err := logger.home()
	if err != nil {
		return nil, err
	}
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
	logger.mu.Lock()
	logger.file = file
	logger.path = path
	logger.dir = dir
	logger.timestamp = timestamp
	logger.mu.Unlock()
	logger.Printf("music-bridge started")
	return logger.Close, nil
}

func (logger *Logger) Close() {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file != nil {
		_ = logger.file.Close()
		logger.file = nil
	}
}

func (logger *Logger) SetContext(context string) error {
	safeContext := SanitizeContext(context)
	if safeContext == "" {
		return nil
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file == nil || logger.path == "" {
		return nil
	}
	nextPath := filepath.Join(logger.dir, "music-bridge-"+safeContext+"-"+logger.timestamp+".log")
	if nextPath == logger.path {
		return nil
	}
	if err := os.Rename(logger.path, nextPath); err != nil {
		return err
	}
	logger.path = nextPath
	return nil
}

func SanitizeContext(value string) string {
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

func (logger *Logger) Writer() io.Writer {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file == nil {
		return logger.stderr
	}
	return io.MultiWriter(logger.stderr, logger.file)
}

func (logger *Logger) Path() string {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.path
}

func (logger *Logger) Printf(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file == nil {
		return
	}
	fmt.Fprintf(logger.file, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}

func (logger *Logger) Sync() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file == nil {
		return nil
	}
	return logger.file.Sync()
}
