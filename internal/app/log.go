package app

import (
	"io"

	"music-bridge/internal/diagnostic"
)

var diagnosticLog = diagnostic.Default

func startDiagnosticLog() (func(), error) {
	return diagnosticLog.Start()
}

func setDiagnosticLogContext(context string) error {
	return diagnosticLog.SetContext(context)
}

func diagnosticWriter() io.Writer {
	return diagnosticLog.Writer()
}

func diagnosticLogPath() string {
	return diagnosticLog.Path()
}

func logf(format string, args ...any) {
	diagnosticLog.Printf(format, args...)
}
