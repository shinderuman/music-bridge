package main

import (
	"fmt"
	"os"

	"music-bridge/internal/bridge"
)

func main() {
	if err := bridge.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "music-bridge:", err)
		if logPath := bridge.DiagnosticLogPath(); logPath != "" {
			fmt.Fprintln(os.Stderr, "music-bridge: 詳細ログ:", logPath)
		}
		bridge.NotifyCompletion()
		os.Exit(1)
	}
	bridge.NotifyCompletion()
}
