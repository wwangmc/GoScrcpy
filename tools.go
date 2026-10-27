package main

import (
	"fmt"
	"net/url"
	"regexp"

	"phonescreen/internal/adb"
)

var packagePattern = regexp.MustCompile(`(?:mResumedActivity|topResumedActivity|mCurrentFocus|mFocusedApp)[^\n]*?\s([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)/`)
var clipboardPackagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)+$`)
var clipboardTokenPattern = regexp.MustCompile(`[A-Za-z0-9_.]+`)
var clipboardURLPattern = regexp.MustCompile(`(?i)(?:https?|market)://[^\s<>"']+`)

func clipboardPackage(text string) (string, error) {
	var name string
	var candidates []string
	text = clipboardURLPattern.ReplaceAllStringFunc(text, func(raw string) string {
		if link, err := url.Parse(raw); err == nil {
			candidates = append(candidates, link.Query().Get("id"))
		}
		return ""
	})
	candidates = append(candidates, clipboardTokenPattern.FindAllString(text, -1)...)
	for _, token := range candidates {
		if !clipboardPackagePattern.MatchString(token) {
			continue
		}
		if name != "" && name != token {
			return "", fmt.Errorf("剪贴板中有多个包名，请只复制目标应用的包名")
		}
		name = token
	}
	if name == "" {
		return "", fmt.Errorf("电脑剪贴板中未找到有效的 Android 包名，请先复制包名，例如 com.chaos.drink.sort")
	}
	return name, nil
}

func currentPackage(output string) (string, error) {
	match := packagePattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return "", fmt.Errorf("未能获取当前应用，请先在手机上打开应用")
	}
	return match[1], nil
}

// Device tools use a fixed argument list, never a host or Android shell command
// assembled from text entered in the UI.
func (a *App) DeviceTool(serial, action, clipboard string) (string, error) {
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
	return androidDeviceTool(run, action, clipboard)
}

func androidDeviceTool(run func(...string) (string, error), action, clipboard string) (string, error) {
	switch action {
	case "ip":
		output, err := run("shell", "ip", "-o", "-4", "addr", "show", "scope", "global")
		if err == nil && output == "" {
			output = "手机当前没有可用的 IPv4 地址。"
		}
		return output, err
	case "package":
		output, err := run("shell", "dumpsys", "activity", "activities")
		if err != nil {
			return "", err
		}
		name, err := currentPackage(output)
		if err != nil {
			return "", err
		}
		return name, nil
	case "store":
		name, err := clipboardPackage(clipboard)
		if err != nil {
			return "", err
		}
		return run("shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "market://details?id="+name)
	default:
		return "", fmt.Errorf("不支持的设备操作")
	}
}
