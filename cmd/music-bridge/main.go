package main

import (
	"fmt"
	"os"

	"music-bridge/internal/bridge"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		bridge.NotifyCompletion()
		os.Exit(2)
	}
	if err := bridge.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "music-bridge:", err)
		bridge.NotifyCompletion()
		os.Exit(1)
	}
	bridge.NotifyCompletion()
}

func usage() {
	fmt.Println("usage: music-bridge {playlists|sync} [--target PATH] [--init-target] [--dry-run] [--refresh]")
}
