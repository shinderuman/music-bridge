package android

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type androidEmulatorTarget struct {
	serial     string
	volumePath string
	volumeKind string
	skipReason string
}

var emulatorTargetOnce sync.Once
var emulatorTarget androidEmulatorTarget

func requireAndroidEmulator(t *testing.T) androidEmulatorTarget {
	t.Helper()
	emulatorTargetOnce.Do(discoverAndroidEmulator)
	if emulatorTarget.skipReason != "" {
		t.Skip(emulatorTarget.skipReason)
	}
	return emulatorTarget
}

func requireIsolatedADB(t *testing.T) {
	t.Helper()
	output, err := exec.Command("adb", "devices").Output()
	if err != nil {
		t.Skipf("ADB device list is unavailable: %v", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" && !strings.HasPrefix(fields[0], "emulator-") {
			t.Skip("transport-loss integration requires an isolated ADB server without physical devices")
		}
	}
}

func discoverAndroidEmulator() {
	if serial := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_SERIAL"); serial != "" {
		volumePath := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_VOLUME")
		if volumePath == "" {
			volumePath = "/storage/emulated/0"
		}
		volumeKind := os.Getenv("MUSIC_BRIDGE_ANDROID_TEST_VOLUME_KIND")
		if volumeKind == "" {
			volumeKind = "内部ストレージ"
		}
		emulatorTarget = androidEmulatorTarget{serial: serial, volumePath: volumePath, volumeKind: volumeKind}
		return
	}
	if _, err := exec.LookPath("adb"); err != nil {
		emulatorTarget.skipReason = "Android SDK platform-tools is not available"
		return
	}
	serial := runningEmulatorSerial()
	if serial == "" {
		emulatorTarget.skipReason = "running Android emulator is unavailable; use make test to start the configured AVD"
		return
	}
	if err := waitForEmulatorBoot(serial, 90*time.Second); err != nil {
		emulatorTarget.skipReason = err.Error()
		return
	}
	var volumes []androidVolume
	var err error
	storageDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(storageDeadline) {
		volumes, err = androidVolumes(serial)
		if err == nil {
			for _, volume := range volumes {
				if volume.Kind == "SDカード" {
					emulatorTarget = androidEmulatorTarget{serial: serial, volumePath: volume.Path, volumeKind: volume.Kind}
					return
				}
			}
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		emulatorTarget.skipReason = fmt.Sprintf("Android emulator storage is unavailable: %v", err)
		return
	}
	for _, volume := range volumes {
		if volume.Kind == "内部ストレージ" {
			emulatorTarget = androidEmulatorTarget{serial: serial, volumePath: volume.Path, volumeKind: volume.Kind}
			return
		}
	}
	emulatorTarget.skipReason = "Android emulator has no writable shared storage"
}

func runningEmulatorSerial() string {
	output, err := exec.Command("adb", "devices").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "emulator-") && fields[1] == "device" {
			return fields[0]
		}
	}
	return ""
}

func waitForEmulatorBoot(serial string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := exec.Command("adb", "-s", serial, "shell", "getprop", "sys.boot_completed").Output()
		if err == nil && strings.TrimSpace(string(output)) == "1" {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Android emulator did not finish booting")
}
