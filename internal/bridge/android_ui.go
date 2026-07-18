package bridge

import (
	"fmt"
	"strings"
)

func chooseAndroidDevice(explicit string) (androidDevice, error) {
	devices, err := adbDevices()
	if err != nil {
		return androidDevice{}, err
	}
	var available []androidDevice
	for _, device := range devices {
		if device.State == "device" && isWirelessAndroidDevice(device) {
			available = append(available, device)
		}
	}
	if explicit != "" {
		for _, device := range available {
			if device.Serial == explicit {
				return device, nil
			}
		}
		return androidDevice{}, fmt.Errorf("指定したWireless debugging端末へ接続されていません: %s", explicit)
	}
	if len(available) == 0 {
		return androidDevice{}, fmt.Errorf("Wireless debuggingで接続済みのAndroid端末がありません。AndroidのWireless debuggingを有効にし、adb pairとadb connectを実行してください")
	}
	if len(available) == 1 {
		return available[0], nil
	}
	items := make([]string, len(available))
	for index, device := range available {
		items[index] = androidDeviceLabel(device)
	}
	index, err := interactiveOne(items, "Android端末を選択してください", func(index int) string {
		return items[index]
	})
	if err != nil {
		return androidDevice{}, err
	}
	return available[index], nil
}

func androidDeviceLabel(device androidDevice) string {
	if device.Model == "" {
		return device.Serial
	}
	return fmt.Sprintf("%s (%s)", androidDeviceName(device), device.Serial)
}

func androidDeviceName(device androidDevice) string {
	if device.Model == "" {
		return device.Serial
	}
	return strings.ReplaceAll(device.Model, "_", " ")
}

func androidDeviceSelectionMessage(device androidDevice) string {
	return "Android端末: " + androidDeviceLabel(device)
}

func androidVolumeSelectionTitle(device androidDevice) string {
	return "Android(" + androidDeviceName(device) + ")の同期先を選択してください"
}

func androidVolumeName(volume androidVolume) string {
	if volume.Label != "" && volume.Label != "null" {
		return volume.Label
	}
	if volume.UUID != "" && volume.UUID != "null" {
		return volume.UUID
	}
	return volume.Kind
}

func chooseAndroidVolume(device androidDevice, explicit string) (androidVolume, error) {
	volumes, err := androidVolumes(device.Serial)
	if err != nil {
		return androidVolume{}, err
	}
	if explicit != "" {
		for _, volume := range volumes {
			if volume.ID == explicit || volume.UUID == explicit || volume.Path == explicit {
				return volume, nil
			}
		}
		return androidVolume{}, fmt.Errorf("指定したAndroidストレージがありません: %s", explicit)
	}
	if len(volumes) == 0 {
		return androidVolume{}, fmt.Errorf("Androidに書き込み可能なストレージがありません")
	}
	items := make([]string, len(volumes))
	for index, volume := range volumes {
		items[index] = androidVolumeLabel(volume)
	}
	index, err := interactiveOne(items, androidVolumeSelectionTitle(device), func(index int) string {
		return items[index]
	})
	if err != nil {
		return androidVolume{}, err
	}
	return volumes[index], nil
}

func androidVolumeLabel(volume androidVolume) string {
	detail := volume.UUID
	if volume.Label != "" && volume.Label != volume.UUID && volume.Label != volume.Kind {
		detail = volume.Label
		if volume.UUID != "" {
			detail += " / " + volume.UUID
		}
	}
	if detail == "" {
		return fmt.Sprintf("%s — %s", volume.Kind, volume.Path)
	}
	if volume.Capacity > 0 {
		return fmt.Sprintf("%s — %s — %s — %s", volume.Kind, detail, humanBytes(volume.Capacity), volume.Path)
	}
	return fmt.Sprintf("%s — %s — %s", volume.Kind, detail, volume.Path)
}
