package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"phonescreen/internal/scrcpy"
)

// Wails v2 owns one native window per process. Each mirror therefore owns its
// own App, USB channels and encoder; stopping one cannot stop another device.
func (a *App) OpenDevice(serial string, options scrcpy.Options) error {
	if a.mirrorSerial != "" {
		return fmt.Errorf("请从连接窗口打开设备")
	}
	if err := options.Validate(); err != nil {
		return err
	}
	devices, err := a.GetDevices()
	if err != nil {
		return err
	}
	ready := false
	for _, d := range devices {
		if d.Serial == serial && d.State == "device" {
			ready = true
		}
	}
	if !ready {
		return fmt.Errorf("设备尚未就绪，请检查连接和手机授权")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "--mirror", serial, "--max-size", strconv.Itoa(options.MaxSize), "--max-fps", strconv.Itoa(options.MaxFPS), "--bit-rate", strconv.Itoa(options.BitRate))
	// This is a GUI executable: hiding its startup window would also hide the
	// newly created mirror. Only ADB helper commands should use HideWindow.
	return a.trackWindow(serial, cmd)
}

func (a *App) trackWindow(serial string, cmd *exec.Cmd) error {
	a.windowsMu.Lock()
	defer a.windowsMu.Unlock()
	if a.windows[serial] {
		return fmt.Errorf("该设备的投屏窗口已打开，请切换到对应窗口")
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法打开投屏窗口：%w", err)
	}
	if a.windows == nil {
		a.windows = make(map[string]bool)
	}
	a.windows[serial] = true
	go func() {
		_ = cmd.Wait()
		a.windowsMu.Lock()
		delete(a.windows, serial)
		a.windowsMu.Unlock()
	}()
	return nil
}

func (a *App) GetOpenDevices() []string {
	a.windowsMu.Lock()
	defer a.windowsMu.Unlock()
	result := make([]string, 0, len(a.windows))
	for serial := range a.windows {
		if strings.HasPrefix(serial, "files:") {
			continue
		}
		result = append(result, serial)
	}
	return result
}

func (a *App) OpenFileManager(serial string) error {
	if _, _, _, err := a.fileBackend(serial, ""); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return a.trackWindow("files:"+serial, exec.Command(executable, "--files", serial))
}
