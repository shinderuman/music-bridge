package android

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type fakeADBExecutor struct {
	output func(args ...string) ([]byte, error)
	run    func(input io.Reader, args ...string) ([]byte, error)
}

type fakeStreamingADBExecutor struct {
	fakeADBExecutor
	stream func(args ...string) (io.ReadCloser, func() ([]byte, error), error)
}

func (fake fakeStreamingADBExecutor) Stream(
	args ...string,
) (io.ReadCloser, func() ([]byte, error), error) {
	return fake.stream(args...)
}

func (fake fakeADBExecutor) Output(args ...string) ([]byte, error) {
	if fake.output == nil {
		return nil, nil
	}
	return fake.output(args...)
}

func (fake fakeADBExecutor) Run(input io.Reader, args ...string) ([]byte, error) {
	if fake.run == nil {
		return nil, nil
	}
	return fake.run(input, args...)
}

func useFakeADB(t *testing.T, fake adbExecutor) {
	t.Helper()
	previous := adbExec
	adbExec = fake
	t.Cleanup(func() { adbExec = previous })
}

func TestParseADBDevices(t *testing.T) {
	data := "List of devices attached\n192.168.0.2:5555 device product:x model:Pixel_9\nemulator-5554 offline\nUSB123 device model:Pixel_USB\n"
	want := []androidDevice{
		{Serial: "192.168.0.2:5555", State: "device", Model: "Pixel_9"},
		{Serial: "emulator-5554", State: "offline"},
		{Serial: "USB123", State: "device", Model: "Pixel_USB"},
	}
	if got := parseADBDevices(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("devices=%#v", got)
	}
}

func TestIsWirelessAndroidDeviceExcludesUSBTransport(t *testing.T) {
	tests := []struct {
		serial string
		want   bool
	}{
		{"192.168.0.2:5555", true},
		{"adb-ABC-xyz._adb-tls-connect._tcp", true},
		{"emulator-5554", true},
		{"R58M123456A", false},
	}
	for _, test := range tests {
		if got := isWirelessAndroidDevice(androidDevice{Serial: test.serial}); got != test.want {
			t.Errorf("isWirelessAndroidDevice(%q)=%v, want %v", test.serial, got, test.want)
		}
	}
}

func TestParseAndroidVolumesClassifiesSDAndUSB(t *testing.T) {
	data := `Disks:
  DiskInfo{disk:7,336}:
    flags=ADOPTABLE|SD size=536870912 label=VirtualSD
    sysPath=/sys/virtual
  DiskInfo{disk:8,0}:
    flags=ADOPTABLE|USB size=64000000000 label=USB_DISK
    sysPath=/sys/block/sda

Volumes:
  VolumeInfo{public:7,337}:
    type=PUBLIC diskId=disk:7,336 partGuid= mountFlags=VISIBLE_FOR_WRITE mountUserId=0 state=MOUNTED
    fsType=vfat fsUuid=2681-1521 fsLabel=
    path=/storage/2681-1521 internalPath=/mnt/media_rw/2681-1521
  VolumeInfo{public:8,1}:
    type=PUBLIC diskId=disk:8,0 partGuid= mountFlags=VISIBLE_FOR_WRITE mountUserId=0 state=MOUNTED
    fsType=exfat fsUuid=ABCD-1234 fsLabel=MUSIC_USB
    path=/storage/ABCD-1234 internalPath=/mnt/media_rw/ABCD-1234
  VolumeInfo{public:8,2}:
    type=PUBLIC diskId=disk:8,0 state=UNMOUNTED
    fsUuid=OFFLINE path=/storage/OFFLINE
`
	want := []androidVolume{
		{ID: "primary", Kind: "内部ストレージ", Path: "/storage/emulated/0", Label: "内部ストレージ"},
		{ID: "public:7,337", DiskID: "disk:7,336", Kind: "SDカード", Path: "/storage/2681-1521", UUID: "2681-1521", Label: "VirtualSD", Capacity: 536870912},
		{ID: "public:8,1", DiskID: "disk:8,0", Kind: "USB OTG", Path: "/storage/ABCD-1234", UUID: "ABCD-1234", Label: "MUSIC_USB", Capacity: 64000000000},
	}
	if got := parseAndroidVolumes(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("volumes=%#v\nwant=%#v", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b c"), `'a'"'"'b c'`; got != want {
		t.Fatalf("shellQuote=%q, want %q", got, want)
	}
}

func TestADBHelpersUseSelectedSerialAndPreserveInput(t *testing.T) {
	var outputArgs, runArgs []string
	var inputData []byte
	useFakeADB(t, fakeADBExecutor{
		output: func(args ...string) ([]byte, error) {
			outputArgs = append([]string(nil), args...)
			return []byte("result"), nil
		},
		run: func(input io.Reader, args ...string) ([]byte, error) {
			runArgs = append([]string(nil), args...)
			inputData, _ = io.ReadAll(input)
			return nil, nil
		},
	})
	out, err := adbShell("device:5555", "echo test")
	if err != nil || string(out) != "result" {
		t.Fatalf("adbShell=%q,%v", out, err)
	}
	if got, want := outputArgs, []string{"-s", "device:5555", "shell", "echo test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("output args=%#v", got)
	}
	if err := adbWrite("device:5555", "/storage/a b", []byte("content")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inputData, []byte("content")) ||
		!reflect.DeepEqual(runArgs[:4], []string{"-s", "device:5555", "shell", "-T"}) ||
		!strings.Contains(runArgs[4], "cat > '/storage/a b'") {
		t.Fatalf("run args=%#v input=%q", runArgs, inputData)
	}
	if got := adbSerialArgs(""); got != nil {
		t.Fatalf("empty serial args=%#v", got)
	}
}

func TestADBDeviceAndVolumeQueries(t *testing.T) {
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "devices -l":
			return []byte("List of devices attached\n10.0.0.2:5555 device model:Pixel\n"), nil
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
			return nil, errors.New("unexpected args")
		}
	}})
	devices, err := adbDevices()
	if err != nil || len(devices) != 1 || devices[0].Model != "Pixel" {
		t.Fatalf("devices=%#v, err=%v", devices, err)
	}
	volumes, err := androidVolumes("10.0.0.2:5555")
	if err != nil || len(volumes) != 2 || volumes[1].Kind != "SDカード" {
		t.Fatalf("volumes=%#v, err=%v", volumes, err)
	}
}

func TestADBErrorsIncludeCommandOutput(t *testing.T) {
	err := adbError("failed", []byte("device offline\n"), errors.New("exit status 1"))
	if !strings.Contains(err.Error(), "device offline") || !isRetryableADBError(err) {
		t.Fatalf("error=%v", err)
	}
}

func TestAndroidLockIdentitiesUseStableDeviceAndVolumeIDs(t *testing.T) {
	if got := parseAndroidDeviceLockIdentity("unknown\nabc123\n", "10.0.0.2:5555"); got != "abc123" {
		t.Fatalf("device identity=%q", got)
	}
	if got := parseAndroidDeviceLockIdentity("unknown\nnull\n", "10.0.0.2:5555"); got != "10.0.0.2:5555" {
		t.Fatalf("device fallback=%q", got)
	}
	if got := androidVolumeLockIdentity(androidVolume{ID: "public:7,337", UUID: "abcd-1234", Path: "/storage/ABCD-1234"}); got != "uuid:ABCD-1234" {
		t.Fatalf("external volume identity=%q", got)
	}
	if got := androidVolumeLockIdentity(androidVolume{ID: "primary", Path: "/storage/emulated/0"}); got != "id:primary" {
		t.Fatalf("internal volume identity=%q", got)
	}
}

func TestAndroidDeviceLockIdentityQueriesSelectedDevice(t *testing.T) {
	useFakeADB(t, fakeADBExecutor{output: func(args ...string) ([]byte, error) {
		if len(args) < 4 || args[0] != "-s" || args[1] != "10.0.0.2:5555" {
			t.Fatalf("args=%#v", args)
		}
		return []byte("hardware-serial\nandroid-id\n"), nil
	}})
	got, err := androidDeviceLockIdentity("10.0.0.2:5555")
	if err != nil || got != "hardware-serial" {
		t.Fatalf("identity=%q, err=%v", got, err)
	}
}
