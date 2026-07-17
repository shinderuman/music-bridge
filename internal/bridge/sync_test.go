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
