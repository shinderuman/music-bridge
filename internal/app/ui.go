package app

import "music-bridge/internal/tui"

type syncMode string

const (
	driveSyncMode   syncMode = "drive"
	androidSyncMode syncMode = "android"
)

var terminalUI = tui.System()

func chooseSyncMode() (syncMode, error) {
	items := []string{
		"ドライブ更新モード（Macに接続したmicroSDXCなど）",
		"Android更新モード（Wireless debugging）",
	}
	index, err := terminalUI.SelectOne(len(items), "更新モードを選択してください", func(index int) string {
		return items[index]
	})
	if err != nil {
		return "", err
	}
	if index == 1 {
		return androidSyncMode, nil
	}
	return driveSyncMode, nil
}
