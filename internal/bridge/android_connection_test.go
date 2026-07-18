package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func useFastAndroidReconnect(t *testing.T, promptAfter time.Duration) {
	t.Helper()
	previousPromptAfter := androidReconnectPromptAfter
	previousBaseDelay := androidReconnectBaseDelay
	previousMaxDelay := androidReconnectMaxDelay
	androidReconnectPromptAfter = promptAfter
	androidReconnectBaseDelay = time.Millisecond
	androidReconnectMaxDelay = time.Millisecond
	t.Cleanup(func() {
		androidReconnectPromptAfter = previousPromptAfter
		androidReconnectBaseDelay = previousBaseDelay
		androidReconnectMaxDelay = previousMaxDelay
	})
}

func TestAndroidReconnectPromptsAgainOneIntervalAfterYes(t *testing.T) {
	useFastAndroidReconnect(t, 20*time.Millisecond)
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		return []byte("device offline"), errors.New("exit status 1")
	}})

	previousNotifier := androidDisconnectNotifier
	previousConfirmation := androidReconnectConfirmation
	notifications := 0
	confirmations := 0
	androidDisconnectNotifier = func() { notifications++ }
	androidReconnectConfirmation = func(string) bool {
		confirmations++
		return confirmations == 1
	}
	t.Cleanup(func() {
		androidDisconnectNotifier = previousNotifier
		androidReconnectConfirmation = previousConfirmation
	})

	connection := newAndroidConnection("device:5555", "Pixel")
	cause := errors.New("device offline")
	if err := connection.Wait(context.Background(), cause); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if err := connection.Wait(context.Background(), cause); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 || confirmations != 1 {
		t.Fatalf("first interval notifications=%d confirmations=%d", notifications, confirmations)
	}

	if err := connection.Wait(context.Background(), cause); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("prompt repeated before another interval: %d", notifications)
	}
	time.Sleep(25 * time.Millisecond)
	err := connection.Wait(context.Background(), cause)
	if err == nil || !strings.Contains(err.Error(), "再接続を中断") {
		t.Fatalf("error=%v", err)
	}
	if notifications != 2 || confirmations != 2 {
		t.Fatalf("second interval notifications=%d confirmations=%d", notifications, confirmations)
	}
}

func TestAndroidReconnectRediscoversSameDeviceWhenSerialChanges(t *testing.T) {
	useFastAndroidReconnect(t, time.Hour)
	const oldSerial = "adb-old._adb-tls-connect._tcp"
	const newSerial = "adb-new._adb-tls-connect._tcp"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "connect "+oldSerial:
			return []byte("failed to connect"), errors.New("exit status 1")
		case joined == "-s "+oldSerial+" get-state":
			return []byte("device not found"), errors.New("exit status 1")
		case joined == "devices -l":
			return []byte("List of devices attached\n" + newSerial + " device model:Pixel_6a\n"), nil
		case strings.HasPrefix(joined, "-s "+newSerial+" shell "):
			return []byte("stable-device-id\nandroid-id\n"), nil
		default:
			t.Fatalf("unexpected adb command: %q", joined)
			return nil, nil
		}
	}})

	connection := newAndroidConnection(oldSerial, "Pixel 6a")
	connection.SetIdentity("stable-device-id")
	if err := connection.Wait(context.Background(), errors.New("device offline")); err != nil {
		t.Fatal(err)
	}
	if got := connection.Serial(); got != newSerial {
		t.Fatalf("serial=%q, want %q", got, newSerial)
	}
}

func TestAndroidReconnectUsesCurrentSerialWhenItRecovers(t *testing.T) {
	const serial = "adb-current._adb-tls-connect._tcp"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "connect " + serial:
			return []byte("already connected"), nil
		case "-s " + serial + " get-state":
			return []byte("device\n"), nil
		default:
			t.Fatalf("unexpected adb command: %#v", args)
			return nil, nil
		}
	}})
	connection := newAndroidConnection(serial, "Pixel")
	if !connection.tryReconnect() {
		t.Fatal("current serial was not reconnected")
	}
	if connection.Serial() != serial {
		t.Fatalf("serial changed to %q", connection.Serial())
	}
}

func TestAndroidReconnectDoesNotSwitchToDifferentDevice(t *testing.T) {
	const oldSerial = "adb-old._adb-tls-connect._tcp"
	const otherSerial = "adb-other._adb-tls-connect._tcp"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "connect "+oldSerial:
			return []byte("failed"), errors.New("exit status 1")
		case joined == "-s "+oldSerial+" get-state":
			return []byte("not found"), errors.New("exit status 1")
		case joined == "devices -l":
			return []byte("List of devices attached\n" + otherSerial + " device model:Other\n"), nil
		case strings.HasPrefix(joined, "-s "+otherSerial+" shell "):
			return []byte("different-device-id\n"), nil
		default:
			t.Fatalf("unexpected adb command: %q", joined)
			return nil, nil
		}
	}})
	connection := newAndroidConnection(oldSerial, "Pixel")
	connection.SetIdentity("wanted-device-id")
	if connection.tryReconnect() {
		t.Fatal("connection switched to a different Android device")
	}
	if connection.Serial() != oldSerial {
		t.Fatalf("serial changed to %q", connection.Serial())
	}
}
