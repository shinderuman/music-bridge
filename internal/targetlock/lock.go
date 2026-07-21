// Package targetlock prevents two processes from syncing the same destination.
package targetlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func Lock(root string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(root, ".music-bridge-sync.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("この同期先は別のmusic-bridgeが同期中です: %s", root)
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
