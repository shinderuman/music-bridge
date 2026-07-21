package main

import (
	"fmt"
	"os"

	"music-bridge/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "music-bridge:", err)
		if logPath := app.DiagnosticLogPath(); logPath != "" {
			fmt.Fprintln(os.Stderr, "music-bridge: 詳細ログ:", logPath)
		}
		app.NotifyCompletion()
		os.Exit(1)
	}
	app.NotifyCompletion()
}
