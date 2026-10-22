package main

import (
	"fmt"
	"regexp"

	"phonescreen/internal/adb"
)

var packagePattern = regexp.MustCompile(`(?:mResumedActivity|topResumedActivity|mCurrentFocus|mFocusedApp)[^\n]*?\s([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)/`)

func currentPackage(output string) (string, error) {
	match := packagePattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return "", fmt.Errorf("未能获取当前应用，请先在手机上打开应用")
	}
	return match[1], nil
}

// Device tools use a fixed argument list, never a host or Android shell command
// assembled from text entered in the UI.
func (a *App) DeviceTool(serial, action string) (string, error) {
	if action == "file" {
		return "", a.OpenFileManager(serial)
	}
	if a.mirrorSerial != "" && a.mirrorSerial != serial {
		return "", fmt.Errorf("此窗口仅能操作对应设备")
	}
	devices, err := a.GetDevices()
	if err != nil {
		return "", err
	}
	android := false
	for _, d := range devices {
		if d.Serial == serial && d.Platform == "android" && d.State == "device" {
			android = true
		}
	}
	if !android {
		return "", fmt.Errorf("此操作需要已授权的 Android 设备")
	}
	client, err := adb.Resolve()
	if err != nil {
		return "", err
	}
	run := func(args ...string) (string, error) {
		return client.Run(a.ctx, append([]string{"-s", serial}, args...)...)
	}
	switch action {
	case "ip":
		output, err := run("shell", "ip", "-o", "-4", "addr", "show", "scope", "global")
		if err == nil && output == "" {
			output = "手机当前没有可用的 IPv4 地址。"
		}
		return output, err
	case "package", "store":
		output, err := run("shell", "dumpsys", "activity", "activities")
		if err != nil {
			return "", err
		}
		name, err := currentPackage(output)
		if err != nil {
			return "", err
		}
		if action == "package" {
			return name, nil
		}
		return run("shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "market://details?id="+name)
	default:
		return "", fmt.Errorf("不支持的设备操作")
	}
}
