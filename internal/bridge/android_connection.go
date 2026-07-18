package bridge

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
}

var androidReconnectPromptAfter = time.Minute
var androidReconnectBaseDelay = time.Second
var androidReconnectMaxDelay = 10 * time.Second
var androidDisconnectNotifier = NotifyCompletion
var androidReconnectConfirmation = confirmDefaultYes

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
	}
	promptAt := connection.disconnectedAt.Add(androidReconnectPromptAfter)
	connection.attempt++
	attempt := connection.attempt
	shouldPrompt := now.Sub(connection.disconnectedAt) >= androidReconnectPromptAfter
	connection.mu.Unlock()

	if shouldPrompt {
		androidDisconnectNotifier()
		fmt.Printf("\nAndroid(%s)との接続が1分以上切れています。\n", connection.name)
		fmt.Println("Android側のWi-FiとWireless debuggingを確認し、必要なら端末を再起動・再接続してください。")
		if !androidReconnectConfirmation("もう1分、再接続を待ちますか？ [Y/n] ") {
			return fmt.Errorf("Androidとの再接続を中断しました: %w", cause)
		}
		connection.mu.Lock()
		connection.disconnectedAt = time.Now()
		connection.attempt = 0
		attempt = 1
		connection.mu.Unlock()
	}

	delay := time.Duration(attempt) * androidReconnectBaseDelay
	if delay > androidReconnectMaxDelay {
		delay = androidReconnectMaxDelay
	}
	if !shouldPrompt {
		remaining := time.Until(promptAt)
		if remaining > 0 && delay > remaining {
			delay = remaining
		}
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
