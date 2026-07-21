package android

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type androidSerial func() string

func fixedAndroidSerial(serial string) androidSerial {
	return func() string { return serial }
}

type androidConnection struct {
	mu             sync.RWMutex
	serial         string
	identity       string
	name           string
	attempt        int
	disconnectedAt time.Time
	nextNoticeAt   time.Time
	firstNotified  bool
}

var androidReconnectFirstNoticeAfter = time.Minute
var androidReconnectNoticeInterval = 5 * time.Minute
var androidReconnectBaseDelay = time.Second
var androidReconnectMaxDelay = 10 * time.Second
var androidDisconnectNotifier = func() { androidNotifier.Completion() }
var androidDisconnectReminder = func() { androidNotifier.AndroidDisconnectReminder() }

func newAndroidConnection(serial, name string) *androidConnection {
	return &androidConnection{serial: serial, name: name}
}

func (connection *androidConnection) Serial() string {
	connection.mu.RLock()
	defer connection.mu.RUnlock()
	return connection.serial
}

func (connection *androidConnection) SetIdentity(identity string) {
	connection.mu.Lock()
	connection.identity = identity
	connection.mu.Unlock()
}

func (connection *androidConnection) Wait(ctx context.Context, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	connection.mu.Lock()
	if connection.disconnectedAt.IsZero() {
		connection.disconnectedAt = now
		connection.nextNoticeAt = now.Add(androidReconnectFirstNoticeAfter)
	}
	connection.attempt++
	attempt := connection.attempt
	firstNotice := !connection.firstNotified && !now.Before(connection.nextNoticeAt)
	repeatedNotice := connection.firstNotified && !now.Before(connection.nextNoticeAt)
	if firstNotice {
		connection.firstNotified = true
		connection.nextNoticeAt = now.Add(androidReconnectNoticeInterval)
	} else if repeatedNotice {
		connection.nextNoticeAt = now.Add(androidReconnectNoticeInterval)
	}
	nextNoticeAt := connection.nextNoticeAt
	connection.mu.Unlock()

	if firstNotice {
		androidDisconnectNotifier()
		fmt.Printf("\nAndroid(%s)との接続が1分以上切れています。\n", connection.name)
		fmt.Println("Android側のWi-FiとWireless debuggingを確認し、必要なら端末を再起動・再接続してください。")
		fmt.Println("接続が戻るまで自動再接続を続けます（Ctrl+Cで中止）。")
	} else if repeatedNotice {
		androidDisconnectReminder()
		fmt.Printf("\nAndroid(%s)との接続が切れたままです。自動再接続を続けています。\n", connection.name)
	}

	delay := time.Duration(attempt) * androidReconnectBaseDelay
	if delay > androidReconnectMaxDelay {
		delay = androidReconnectMaxDelay
	}
	remaining := time.Until(nextNoticeAt)
	if remaining > 0 && delay > remaining {
		delay = remaining
	}
	fmt.Printf("\033[2K\rAndroidとの接続が切れました。再接続待機中... %s（Ctrl+Cで中止）", delay)
	logf("Android reconnect wait %d: %v", attempt, cause)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}
	if connection.tryReconnect() {
		connection.mu.Lock()
		connection.attempt = 0
		connection.disconnectedAt = time.Time{}
		connection.nextNoticeAt = time.Time{}
		connection.firstNotified = false
		connection.mu.Unlock()
		fmt.Print("\033[2K\rAndroidへ再接続しました。処理を再開します。\n")
	}
	return nil
}

func (connection *androidConnection) tryReconnect() bool {
	serial := connection.Serial()
	if !strings.HasPrefix(serial, "emulator-") {
		_, _ = adbExec.Output("connect", serial)
	}
	if out, err := adbOutput(serial, "get-state"); err == nil &&
		strings.TrimSpace(string(out)) == "device" {
		return true
	}

	connection.mu.RLock()
	identity := connection.identity
	connection.mu.RUnlock()
	if identity == "" {
		return false
	}
	devices, err := adbDevices()
	if err != nil {
		return false
	}
	for _, device := range devices {
		if device.State != "device" || !isWirelessAndroidDevice(device) {
			continue
		}
		candidateIdentity, err := androidDeviceLockIdentity(device.Serial)
		if err != nil || candidateIdentity != identity {
			continue
		}
		connection.mu.Lock()
		connection.serial = device.Serial
		connection.mu.Unlock()
		logf("Android reconnect serial changed: %s", device.Serial)
		return true
	}
	return false
}

func waitForAndroid(serial string) androidReconnectWait {
	return newAndroidConnection(serial, serial).Wait
}
