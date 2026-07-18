package bridge

import (
	"strings"
	"testing"
)

func TestRunSyncRejectsRemovedSubcommands(t *testing.T) {
	for _, argument := range []string{"sync", "playlists"} {
		err := runSync([]string{argument})
		if err == nil || !strings.Contains(err.Error(), "サブコマンドは廃止されました") {
			t.Errorf("runSync(%q) error = %v, want removed-subcommand error", argument, err)
		}
	}
}

func TestRunTreatsHelpAsSuccessfulExit(t *testing.T) {
	if err := Run([]string{"--help"}); err != nil {
		t.Fatalf("help error=%v", err)
	}
}

func TestRunSyncRejectsRemovedAndroidFlag(t *testing.T) {
	for _, flag := range []string{"--android", "--target", "--android-device", "--android-storage"} {
		err := runSync([]string{flag})
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
			t.Errorf("%s error=%v", flag, err)
		}
	}
}
