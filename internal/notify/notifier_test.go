package notify

import (
	"bytes"
	"reflect"
	"testing"
)

func TestCompletionRingsTerminalAndPlaysSound(t *testing.T) {
	var output bytes.Buffer
	var command []string
	notifier := NewWithRunner(&output, func(name string, args ...string) error {
		command = append([]string{name}, args...)
		return nil
	})
	notifier.Completion()
	if output.String() != "\a" {
		t.Fatalf("output=%q", output.String())
	}
	if want := []string{"afplay", completionSound}; !reflect.DeepEqual(command, want) {
		t.Fatalf("command=%#v, want %#v", command, want)
	}
}

func TestAndroidDisconnectReminderUsesNotificationCenter(t *testing.T) {
	var command []string
	notifier := NewWithRunner(&bytes.Buffer{}, func(name string, args ...string) error {
		command = append([]string{name}, args...)
		return nil
	})
	notifier.AndroidDisconnectReminder()
	if len(command) != 3 || command[0] != "osascript" || command[1] != "-e" {
		t.Fatalf("command=%#v", command)
	}
	if command[2] == "" {
		t.Fatal("notification script is empty")
	}
}
