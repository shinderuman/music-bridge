package bridge

import (
	"errors"
	"strings"
	"testing"
)

func TestAndroidDeviceLabel(t *testing.T) {
	device := androidDevice{Serial: "192.168.1.2:5555", Model: "Pixel_9_Pro"}
	if got := androidDeviceLabel(device); got != "Pixel 9 Pro (192.168.1.2:5555)" {
		t.Fatalf("label=%q", got)
	}
	if got := androidDeviceSelectionMessage(device); got != "Android端末: Pixel 9 Pro (192.168.1.2:5555)" {
		t.Fatalf("selection message=%q", got)
	}
	if got := androidVolumeSelectionTitle(device); got != "Android(Pixel 9 Pro)の同期先を選択してください" {
		t.Fatalf("volume selection title=%q", got)
	}
	if got := androidVolumeName(androidVolume{Kind: "SDカード", Label: "SDXC128GB"}); got != "SDXC128GB" {
		t.Fatalf("volume name=%q", got)
	}
	if got := androidVolumeName(androidVolume{Kind: "SDカード", UUID: "ABCD-1234"}); got != "ABCD-1234" {
		t.Fatalf("UUID fallback=%q", got)
	}
	if got := androidVolumeName(androidVolume{Kind: "内部ストレージ"}); got != "内部ストレージ" {
		t.Fatalf("kind fallback=%q", got)
	}
	serialOnly := androidDevice{Serial: "10.0.0.2:5555"}
	if got := androidDeviceLabel(serialOnly); got != serialOnly.Serial {
		t.Fatalf("serial-only label=%q", got)
	}
}

func TestAndroidVolumeLabelDistinguishesSameKindVolumes(t *testing.T) {
	first := androidVolume{Kind: "USB OTG", UUID: "AAAA-1111", Label: "DISK_A", Path: "/storage/AAAA-1111", Capacity: 64 << 30}
	second := androidVolume{Kind: "USB OTG", UUID: "BBBB-2222", Label: "DISK_B", Path: "/storage/BBBB-2222", Capacity: 128 << 30}
	firstLabel, secondLabel := androidVolumeLabel(first), androidVolumeLabel(second)
	if firstLabel == secondLabel {
		t.Fatalf("labels collide: %q", firstLabel)
	}
	for _, part := range []string{"USB OTG", "DISK_A", "AAAA-1111", "/storage/AAAA-1111"} {
		if !strings.Contains(firstLabel, part) {
			t.Errorf("first label %q does not contain %q", firstLabel, part)
		}
	}
}

func TestChooseExplicitAndroidDeviceAndVolume(t *testing.T) {
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "devices -l":
			return []byte("List of devices attached\n10.0.0.2:5555 device model:Pixel\nUSB123 device model:USB\n"), nil
		case "-s 10.0.0.2:5555 shell dumpsys mount":
			return []byte(`Disks:
  DiskInfo{disk:7,336}:
    flags=SD size=100 label=SD
    sysPath=/sys/virtual
Volumes:
  VolumeInfo{public:7,337}:
    diskId=disk:7,336 state=MOUNTED fsUuid=ABCD-1234
    path=/storage/ABCD-1234
`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}})
	device, err := chooseAndroidDevice("10.0.0.2:5555")
	if err != nil || device.Model != "Pixel" {
		t.Fatalf("device=%#v, err=%v", device, err)
	}
	volume, err := chooseAndroidVolume(device, "ABCD-1234")
	if err != nil || volume.Kind != "SDカード" {
		t.Fatalf("volume=%#v, err=%v", volume, err)
	}
}

func TestChooseAndroidDeviceRejectsUSBOnlyConnection(t *testing.T) {
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		return []byte("List of devices attached\nUSB123 device model:Pixel\n"), nil
	}})
	if _, err := chooseAndroidDevice(""); err == nil || !strings.Contains(err.Error(), "Wireless debugging") {
		t.Fatalf("error=%v", err)
	}
}

func TestChooseAndroidDeviceReturnsSingleWirelessDeviceWithoutPrompt(t *testing.T) {
	const serial = "adb-ABC._adb-tls-connect._tcp"
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		return []byte("List of devices attached\n" + serial + " device model:Pixel_6a\n"), nil
	}})
	device, err := chooseAndroidDevice("")
	if err != nil {
		t.Fatal(err)
	}
	if device.Serial != serial || device.Model != "Pixel_6a" {
		t.Fatalf("device=%#v", device)
	}
}
