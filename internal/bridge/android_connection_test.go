package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func useFastAndroidReconnect(t *testing.T, firstNoticeAfter, noticeInterval time.Duration) {
	t.Helper()
	previousFirstNoticeAfter := androidReconnectFirstNoticeAfter
	previousNoticeInterval := androidReconnectNoticeInterval
	previousBaseDelay := androidReconnectBaseDelay
	previousMaxDelay := androidReconnectMaxDelay
	androidReconnectFirstNoticeAfter = firstNoticeAfter
	androidReconnectNoticeInterval = noticeInterval
	androidReconnectBaseDelay = time.Millisecond
	androidReconnectMaxDelay = time.Millisecond
	t.Cleanup(func() {
		androidReconnectFirstNoticeAfter = previousFirstNoticeAfter
		androidReconnectNoticeInterval = previousNoticeInterval
		androidReconnectBaseDelay = previousBaseDelay
		androidReconnectMaxDelay = previousMaxDelay
	})
}

func TestAndroidReconnectNotifiesAndRetriesForever(t *testing.T) {
	useFastAndroidReconnect(t, 20*time.Millisecond, 30*time.Millisecond)
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		return []byte("device offline"), errors.New("exit status 1")
	}})

	previousNotifier := androidDisconnectNotifier
	previousReminder := androidDisconnectReminder
	firstNotifications := 0
	reminders := 0
	androidDisconnectNotifier = func() { firstNotifications++ }
	androidDisconnectReminder = func() { reminders++ }
	t.Cleanup(func() {
		androidDisconnectNotifier = previousNotifier
		androidDisconnectReminder = previousReminder
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
	if firstNotifications != 1 || reminders != 0 {
		t.Fatalf("first interval notifications=%d reminders=%d", firstNotifications, reminders)
	}

	if err := connection.Wait(context.Background(), cause); err != nil {
		t.Fatal(err)
	}
	if firstNotifications != 1 || reminders != 0 {
		t.Fatalf("notification repeated early: first=%d reminders=%d", firstNotifications, reminders)
	}
	time.Sleep(35 * time.Millisecond)
	if err := connection.Wait(context.Background(), cause); err != nil {
		t.Fatal(err)
	}
	if firstNotifications != 1 || reminders != 1 {
		t.Fatalf("repeat interval notifications=%d reminders=%d", firstNotifications, reminders)
	}
}

func TestAndroidReconnectRediscoversSameDeviceWhenSerialChanges(t *testing.T) {
	useFastAndroidReconnect(t, time.Hour, time.Hour)
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
