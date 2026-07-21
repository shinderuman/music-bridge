package drive

import (
	"io"

	"music-bridge/internal/diagnostic"
)

func setDiagnosticLogContext(context string) error { return diagnostic.Default.SetContext(context) }
func diagnosticWriter() io.Writer                  { return diagnostic.Default.Writer() }
func logf(format string, args ...any)              { diagnostic.Default.Printf(format, args...) }
