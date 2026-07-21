package android

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type androidDevice struct {
	Serial string
	State  string
	Model  string
}

type androidVolume struct {
	ID       string
	DiskID   string
	Kind     string
	Path     string
	UUID     string
	Label    string
	Capacity int64
}

type adbExecutor interface {
	Output(args ...string) ([]byte, error)
	Run(stdin io.Reader, args ...string) ([]byte, error)
}

type adbStreamingExecutor interface {
	Stream(args ...string) (io.ReadCloser, func() ([]byte, error), error)
}

type systemADBExecutor struct{}

func (systemADBExecutor) Output(args ...string) ([]byte, error) {
	return exec.Command("adb", args...).CombinedOutput()
}

func (systemADBExecutor) Run(stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("adb", args...)
	cmd.Stdin = stdin
	return cmd.CombinedOutput()
}

func (systemADBExecutor) Stream(args ...string) (io.ReadCloser, func() ([]byte, error), error) {
	cmd := exec.Command("adb", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	wait := func() ([]byte, error) {
		err := cmd.Wait()
		return stderr.Bytes(), err
	}
	return stdout, wait, nil
}

var adbExec adbExecutor = systemADBExecutor{}

func parseADBDevices(data string) []androidDevice {
	var devices []androidDevice
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "List" {
			continue
		}
		device := androidDevice{Serial: fields[0], State: fields[1]}
		for _, field := range fields[2:] {
			if strings.HasPrefix(field, "model:") {
				device.Model = strings.TrimPrefix(field, "model:")
			}
		}
		devices = append(devices, device)
	}
	return devices
}

func isWirelessAndroidDevice(device androidDevice) bool {
	if strings.HasPrefix(device.Serial, "emulator-") {
		return true
	}
	return strings.Contains(device.Serial, ":") ||
		strings.Contains(device.Serial, "._adb-tls-connect._tcp")
}

func adbDevices() ([]androidDevice, error) {
	out, err := adbExec.Output("devices", "-l")
	if err != nil {
		return nil, adbError("adbデバイス一覧を取得できませんでした", out, err)
	}
	return parseADBDevices(string(out)), nil
}

var (
	diskHeaderPattern   = regexp.MustCompile(`DiskInfo\{([^}]+)\}`)
	volumeHeaderPattern = regexp.MustCompile(`VolumeInfo\{([^}]+)\}`)
	keyValuePattern     = regexp.MustCompile(`([A-Za-z][A-Za-z0-9]*)=([^ ]*)`)
)

type androidDisk struct {
	ID       string
	Kind     string
	Label    string
	Capacity int64
}

func parseAndroidVolumes(data string) []androidVolume {
	disks := map[string]androidDisk{}
	var disk *androidDisk
	var volume *androidVolume
	var volumeMounted bool
	var publicVolumes []androidVolume

	flushDisk := func() {
		if disk != nil && disk.ID != "" {
			disks[disk.ID] = *disk
		}
		disk = nil
	}
	flushVolume := func() {
		if volume != nil && volumeMounted && strings.HasPrefix(volume.ID, "public:") &&
			volume.Path != "" && volume.UUID != "" {
			publicVolumes = append(publicVolumes, *volume)
		}
		volume = nil
		volumeMounted = false
	}

	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if match := diskHeaderPattern.FindStringSubmatch(line); len(match) == 2 {
			flushDisk()
			flushVolume()
			disk = &androidDisk{ID: match[1], Kind: "種別不明"}
			continue
		}
		if match := volumeHeaderPattern.FindStringSubmatch(line); len(match) == 2 {
			flushDisk()
			flushVolume()
			volume = &androidVolume{ID: match[1]}
			continue
		}
		if disk != nil {
			if strings.HasPrefix(line, "sysPath=") {
				// DiskInfoの終端。sysPath自体は同期先表示には使わない。
				flushDisk()
				continue
			}
			for _, match := range keyValuePattern.FindAllStringSubmatch(line, -1) {
				switch match[1] {
				case "flags":
					switch {
					case strings.Contains(match[2], "USB"):
						disk.Kind = "USB OTG"
					case strings.Contains(match[2], "SD"):
						disk.Kind = "SDカード"
					}
				case "size":
					disk.Capacity, _ = strconv.ParseInt(match[2], 10, 64)
				case "label":
					disk.Label = match[2]
				}
			}
			continue
		}
		if volume != nil {
			for _, match := range keyValuePattern.FindAllStringSubmatch(line, -1) {
				switch match[1] {
				case "diskId":
					volume.DiskID = match[2]
				case "state":
					volumeMounted = match[2] == "MOUNTED"
				case "fsUuid":
					volume.UUID = match[2]
				case "fsLabel":
					volume.Label = match[2]
				case "path":
					volume.Path = match[2]
				}
			}
		}
	}
	flushDisk()
	flushVolume()

	volumes := []androidVolume{{
		ID:    "primary",
		Kind:  "内部ストレージ",
		Path:  "/storage/emulated/0",
		Label: "内部ストレージ",
	}}
	for _, public := range publicVolumes {
		if info, ok := disks[public.DiskID]; ok {
			public.Kind = info.Kind
			public.Capacity = info.Capacity
			if public.Label == "" || public.Label == "null" {
				public.Label = info.Label
			}
		}
		if public.Kind == "" {
			public.Kind = "種別不明"
		}
		if public.Label == "" || public.Label == "null" {
			public.Label = public.UUID
		}
		volumes = append(volumes, public)
	}
	return volumes
}

func androidVolumes(serial string) ([]androidVolume, error) {
	out, err := adbOutput(serial, "shell", "dumpsys", "mount")
	if err != nil {
		return nil, adbError("Androidストレージ一覧を取得できませんでした", out, err)
	}
	return parseAndroidVolumes(string(out)), nil
}

func androidDeviceLockIdentity(serial string) (string, error) {
	command := `hardware=$(getprop ro.serialno 2>/dev/null); android=$(settings get secure android_id 2>/dev/null); printf '%s\n%s\n' "$hardware" "$android"`
	out, err := adbShell(serial, command)
	if err != nil {
		return "", adbError("Android端末IDを取得できませんでした", out, err)
	}
	return parseAndroidDeviceLockIdentity(string(out), serial), nil
}

func parseAndroidDeviceLockIdentity(data, fallback string) string {
	for _, value := range strings.Split(data, "\n") {
		value = strings.TrimSpace(value)
		if value != "" && value != "unknown" && value != "null" {
			return value
		}
	}
	return fallback
}

func androidVolumeLockIdentity(volume androidVolume) string {
	if volume.UUID != "" && volume.UUID != "null" {
		return "uuid:" + strings.ToUpper(volume.UUID)
	}
	if volume.ID != "" {
		return "id:" + volume.ID
	}
	return "path:" + volume.Path
}

func adbShell(serial string, command string) ([]byte, error) {
	return adbOutput(serial, "shell", command)
}

func adbOutput(serial string, args ...string) ([]byte, error) {
	base := adbSerialArgs(serial)
	base = append(base, args...)
	return adbExec.Output(base...)
}

func adbInput(serial string, input io.Reader, args ...string) ([]byte, error) {
	base := adbSerialArgs(serial)
	base = append(base, args...)
	return adbExec.Run(input, base...)
}

func adbStreamOutput(
	serial string,
	args ...string,
) (io.ReadCloser, func() ([]byte, error), bool, error) {
	streaming, ok := adbExec.(adbStreamingExecutor)
	if !ok {
		return nil, nil, false, nil
	}
	base := adbSerialArgs(serial)
	base = append(base, args...)
	reader, wait, err := streaming.Stream(base...)
	return reader, wait, true, err
}

func adbSerialArgs(serial string) []string {
	if serial == "" {
		return nil
	}
	return []string{"-s", serial}
}

func adbError(message string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", message, err)
	}
	return fmt.Errorf("%s: %w: %s", message, err, detail)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func adbWrite(serial, destination string, data []byte) error {
	command := "cat > " + shellQuote(destination)
	out, err := adbInput(serial, bytes.NewReader(data), "shell", "-T", command)
	if err != nil {
		return adbError("Androidへファイルを書き込めませんでした", out, err)
	}
	return nil
}
