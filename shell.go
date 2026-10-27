package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"phonescreen/internal/adb"
	"phonescreen/internal/mobileshell"
	"phonescreen/internal/usbmux"
)

type ShellLaunch struct {
	MissingOpenSSH bool   `json:"missingOpenSSH"`
	Warning        string `json:"warning,omitempty"`
}

func (a *App) shellDevice(serial string) (string, error) {
	if serial == "" || len(serial) > 256 || strings.ContainsAny(serial, "\x00\r\n") || a.fileSerial != "" || a.mirrorSerial != "" && serial != a.mirrorSerial {
		return "", errors.New("此窗口只能打开对应手机的 Shell")
	}
	devices, err := a.GetDevices()
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.Serial == serial && (d.Platform == "ios" || d.Platform == "android" && d.State == "device") {
			return d.Platform, nil
		}
	}
	return "", errors.New("手机未连接或 ADB 未授权")
}
func usbDevice(ctx context.Context, mux *usbmux.Client, serial string) (usbmux.Device, error) {
	devices, err := mux.Devices(ctx)
	if err != nil {
		return usbmux.Device{}, err
	}
	for _, d := range devices {
		if d.Properties.Serial == serial {
			return d, nil
		}
	}
	return usbmux.Device{}, errors.New("未找到对应 iPhone 的 USB 连接")
}

func (a *App) OpenShell(serial string) (ShellLaunch, error) {
	platform, err := a.shellDevice(serial)
	if err != nil {
		return ShellLaunch{}, err
	}
	result := ShellLaunch{}
	if platform == "ios" {
		ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
		defer cancel()
		device, err := usbDevice(ctx, a.usbmux, serial)
		if err != nil {
			return result, err
		}
		// A working SSH port is sufficient even if PhoneVideo is unavailable.
		conn, err := a.usbmux.Connect(ctx, device, 22)
		if err == nil {
			_ = conn.Close()
		} else {
			api, e := a.iosAPI(ctx, serial)
			if e == nil {
				output, e := api.JSON(ctx, "/runShell", map[string]string{"cmd": mobileshell.ShellCommand(mobileshell.ProbeCommand)})
				api.Close()
				if e == nil {
					output, e = mobileshell.ShellOutput(output)
				}
				if e == nil && output == "missing" {
					result.MissingOpenSSH = true
					return result, nil
				}
				if e == nil && output == "installed" {
					result.Warning = "OpenSSH 已安装，但手机 22 端口未响应。请检查 sshd 服务，或在命令行输入自定义 SSH 端口。"
				}
			}
			if result.Warning == "" {
				result.Warning = "未能确认手机 OpenSSH 状态，请在命令行检查连接和 SSH 端口。"
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return result, err
	}
	err = a.trackWindow("shell:"+serial, exec.Command(executable, "--shell", serial, "--shell-platform", platform))
	return result, err
}

func (a *App) OpenOpenSSHStore(serial, store string) error {
	if _, err := mobileshell.OpenSSHStoreURL(store); err != nil {
		return err
	}
	platform, err := a.shellDevice(serial)
	if err != nil {
		return err
	}
	if platform != "ios" {
		return errors.New("仅 iOS 手机支持跳转 OpenSSH 商店页面")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	api, err := a.iosAPI(ctx, serial)
	if err != nil {
		return err
	}
	defer api.Close()
	return mobileshell.OpenSSHStore(ctx, api, store)
}

type shellConsole struct{ in, out *os.File }

func (c *shellConsole) prompt(reader *bufio.Reader, label, defaultValue string) (string, error) {
	fmt.Fprintf(c.out, "%s [%s]: ", label, defaultValue)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = defaultValue
	}
	return line, nil
}

func runNativeShell(serial, platform string) error {
	console, err := openShellConsole("GoScrcpy Shell - " + serial)
	if err != nil {
		return err
	}
	defer console.in.Close()
	defer console.out.Close()
	reader := bufio.NewReader(console.in)
	fmt.Fprintf(console.out, "GoScrcpy · %s Shell\r\n设备：%s\r\n\r\n", strings.ToUpper(platform), serial)
	if platform == "android" {
		client, e := adb.Resolve()
		err = e
		if e == nil {
			command := exec.Command(client.Path, "-s", serial, "shell", "-tt")
			command.Stdin = console.in
			command.Stdout = console.out
			command.Stderr = console.out
			err = command.Run()
		}
	} else if platform == "ios" {
		err = console.iosShell(reader, serial)
	} else {
		err = errors.New("不支持的手机平台")
	}
	if err != nil {
		fmt.Fprintf(console.out, "\r\n连接或会话结束：%v\r\n", err)
	}
	fmt.Fprint(console.out, "\r\n会话已结束。按 Enter 关闭窗口…")
	_, _ = reader.ReadString('\n')
	return nil
}

var sshUserPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

func (c *shellConsole) iosShell(reader *bufio.Reader, serial string) error {
	fmt.Fprintln(c.out, "通过 USB 连接手机 OpenSSH。请输入手机的 SSH 账号及端口。")
	user, err := c.prompt(reader, "SSH 用户名", "root")
	if err != nil {
		return err
	}
	if !sshUserPattern.MatchString(user) {
		return errors.New("无效 SSH 用户名")
	}
	portText, err := c.prompt(reader, "手机 SSH 端口", "22")
	if err != nil {
		return err
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return errors.New("SSH 端口必须在 1 到 65535 之间")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	mux := usbmux.NewClient()
	device, err := usbDevice(ctx, mux, serial)
	if err != nil {
		return err
	}
	conn, err := mux.Connect(ctx, device, uint16(port))
	if err != nil {
		return fmt.Errorf("无法连接手机 OpenSSH，请确认已安装并启动 sshd，且端口正确：%w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	config := &ssh.ClientConfig{User: user, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		// Host trust is decided in the interactive console before requesting a password.
		_ = conn.SetDeadline(time.Time{})
		err := c.trustSSHKey(reader, serial, ssh.FingerprintSHA256(key))
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		return err
	}, Auth: []ssh.AuthMethod{ssh.PasswordCallback(func() (string, error) {
		_ = conn.SetDeadline(time.Time{})
		fmt.Fprintf(c.out, "%s 的 SSH 密码（输入不显示）: ", user)
		password, err := term.ReadPassword(int(c.in.Fd()))
		fmt.Fprint(c.out, "\r\n")
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		value := string(password)
		for i := range password {
			password[i] = 0
		}
		return value, err
	})}}
	cols, rows, err := term.GetSize(int(c.out.Fd()))
	if err != nil {
		cols, rows = 100, 30
	}
	var raw *term.State
	defer func() {
		if raw != nil {
			_ = term.Restore(int(c.in.Fd()), raw)
		}
	}()
	session, err := mobileshell.OpenSSH(conn, config, cols, rows, c.in, c.out, func() error { var e error; raw, e = term.MakeRaw(int(c.in.Fd())); return e })
	if err != nil {
		return fmt.Errorf("SSH 登录失败：%w", err)
	}
	defer session.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w, h, e := term.GetSize(int(c.out.Fd()))
				if e == nil && (w != cols || h != rows) {
					_ = session.Resize(w, h)
					cols, rows = w, h
				}
			}
		}
	}()
	return session.Wait()
}

func (c *shellConsole) trustSSHKey(reader *bufio.Reader, serial, fingerprint string) error {
	sum := sha256.Sum256([]byte(serial))
	name := "ssh-host-" + hex.EncodeToString(sum[:]) + ".json"
	preferences := &App{}
	var known struct {
		Fingerprint string `json:"fingerprint"`
	}
	err := preferences.readPreference(name, &known)
	if err == nil && known.Fingerprint == fingerprint {
		return nil
	}
	if err == nil {
		return errors.New("此手机 SSH 主机密钥发生变化，已停止连接。请确认是否重新安装过 OpenSSH，再处理对应的 SSH 信任记录。")
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("读取 SSH 信任记录失败：%w", err)
	}
	fmt.Fprintf(c.out, "首次连接此手机，SSH 指纹：%s\r\n", fingerprint)
	answer, err := c.prompt(reader, "确认信任此手机？输入 yes 继续", "no")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("未确认 SSH 主机指纹，已取消连接")
	}
	known.Fingerprint = fingerprint
	return preferences.writePreference(name, known)
}
