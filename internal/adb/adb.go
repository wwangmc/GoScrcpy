package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"phonescreen/internal/device"
	"phonescreen/internal/process"
)

type Device = device.Device

type Client struct{ Path string }

func Resolve() (Client, error) {
	if p := os.Getenv("ADB_PATH"); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return Client{p}, nil
		}
		return Client{}, fmt.Errorf("ADB_PATH 指定的文件不存在: %s", p)
	}
	exe, _ := os.Executable()
	for _, p := range []string{filepath.Join(filepath.Dir(exe), "adb.exe"), filepath.Join(filepath.Dir(exe), "tools", "adb.exe"), filepath.Join("tools", "adb.exe")} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			absolute, _ := filepath.Abs(p)
			return Client{absolute}, nil
		}
	}
	if p, err := exec.LookPath("adb"); err == nil {
		return Client{p}, nil
	}
	for _, name := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(name); root != "" {
			for _, n := range []string{"adb.exe", "adb"} {
				p := filepath.Join(root, "platform-tools", n)
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					return Client{p}, nil
				}
			}
		}
	}
	return Client{}, fmt.Errorf("未找到 ADB。请将 platform-tools 加入 PATH，或设置 ADB_PATH")
}

func (c Client) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	process.HideWindow(cmd)
	return cmd
}

func (c Client) Run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	output, err := c.Command(ctx, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("adb %s: %w — %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (c Client) Devices(ctx context.Context) ([]Device, error) {
	out, err := c.Run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return ParseDevices(out), nil
}

func ParseDevices(output string) []Device {
	devices := make([]Device, 0)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		switch fields[1] {
		case "device", "offline", "unauthorized", "recovery", "sideload", "bootloader", "no":
		default:
			continue
		}
		d := Device{Serial: fields[0], State: fields[1], Model: fields[0], Transport: "USB", Platform: "android", Codec: "h264"}
		if strings.Contains(d.Serial, ":") || strings.Contains(d.Serial, "._tcp") {
			d.Transport = "网络"
		}
		for _, field := range fields[2:] {
			key, value, found := strings.Cut(field, ":")
			if !found {
				continue
			}
			switch key {
			case "model":
				d.Model = strings.ReplaceAll(value, "_", " ")
			case "product":
				d.Product = value
			}
		}
		devices = append(devices, d)
	}
	return devices
}
