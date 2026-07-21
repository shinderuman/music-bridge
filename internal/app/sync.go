package app

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"music-bridge/internal/android"
	"music-bridge/internal/drive"
	"music-bridge/internal/notify"
)

var notifier = notify.New(os.Stderr)

func Run(argv []string) error {
	closeLog, logErr := startDiagnosticLog()
	if logErr == nil {
		defer closeLog()
	}
	err := runSync(argv)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		logf("error: %v", err)
	}
	return err
}

func DiagnosticLogPath() string { return diagnosticLogPath() }

func NotifyCompletion() { notifier.Completion() }

func runSync(argv []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	initTarget := fs.Bool("init-target", false, "initialize target")
	dryRun := fs.Bool("dry-run", false, "dry run")
	refresh := fs.Bool("refresh", false, "refresh playlist cache from Music.app")
	source := fs.String("source-json", "", "JSON source")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("サブコマンドは廃止されました。music-bridge [options] で実行してください")
	}
	mode, err := chooseSyncMode()
	if err != nil {
		return err
	}
	if mode == androidSyncMode {
		return android.Run(android.Options{
			InitTarget: *initTarget,
			DryRun:     *dryRun,
			Refresh:    *refresh,
			Source:     *source,
		})
	}
	return drive.Run(drive.Options{
		InitTarget: *initTarget,
		DryRun:     *dryRun,
		Refresh:    *refresh,
		Source:     *source,
	})
}
