package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"phonescreen/internal/adb"
	"phonescreen/internal/mks"
	"phonescreen/internal/proxyip"
)

type proxyService struct {
	mu    sync.Mutex
	busy  bool
	lines []proxyip.Line
}
type ProxyLines struct {
	Lines   []proxyip.PublicLine `json:"lines"`
	Skipped int                  `json:"skipped"`
}
type ProxyChange struct {
	Message   string `json:"message"`
	Pending   bool   `json:"pending"`
	ProxyName string `json:"proxyName"`
	Region    string `json:"region"`
}

func (a *App) beginProxyOperation() (func(), error) {
	a.proxy.mu.Lock()
	defer a.proxy.mu.Unlock()
	if a.proxy.busy {
		return nil, errors.New("正在执行手机操作，请等待完成")
	}
	a.proxy.busy = true
	return func() { a.proxy.mu.Lock(); a.proxy.busy = false; a.proxy.mu.Unlock() }, nil
}
func (a *App) proxySerial(serial string) error {
	if serial == "" || len(serial) > 256 || (a.mirrorSerial != "" && serial != a.mirrorSerial) || a.fileSerial != "" {
		return errors.New("此窗口只能操作对应投屏手机的代理")
	}
	return nil
}
func proxyPreferenceName(serial string) string {
	sum := sha256.Sum256([]byte(serial))
	return "proxy-" + hex.EncodeToString(sum[:]) + ".json"
}
func (a *App) GetProxyOptions(serial string) (proxyip.Options, error) {
	if err := a.proxySerial(serial); err != nil {
		return proxyip.Options{}, err
	}
	o := proxyip.Defaults()
	if err := a.readPreference(proxyPreferenceName(serial), &o); err != nil && !os.IsNotExist(err) {
		return o, err
	}
	if _, err := proxyip.Normalize(o); err != nil {
		return proxyip.Defaults(), nil
	}
	return o, nil
}
func (a *App) RememberProxyOptions(serial string, options proxyip.Options) error {
	if err := a.proxySerial(serial); err != nil {
		return err
	}
	o, err := proxyip.Normalize(options)
	if err != nil {
		return err
	}
	// Only selection, region and app/name are persisted. Provider secrets stay in memory.
	return a.writePreference(proxyPreferenceName(serial), o)
}
func (a *App) LoadProxyLines() (ProxyLines, error) {
	done, err := a.beginProxyOperation()
	if err != nil {
		return ProxyLines{}, err
	}
	defer done()
	ctx, cancel := context.WithTimeout(a.ctx, 18*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyip.ListURL, nil)
	if err != nil {
		return ProxyLines{}, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	client := &http.Client{Timeout: 18 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return ProxyLines{}, errors.New("无法加载代理列表，请检查电脑网络后刷新")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ProxyLines{}, fmt.Errorf("代理接口返回 HTTP %d", response.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(b) > 2*1024*1024 {
		return ProxyLines{}, errors.New("读取代理列表失败或列表过大")
	}
	lines, skipped, err := proxyip.ParseLines(b)
	if err != nil {
		return ProxyLines{}, err
	}
	a.proxy.mu.Lock()
	a.proxy.lines = lines
	a.proxy.mu.Unlock()
	public := make([]proxyip.PublicLine, 0, len(lines))
	for _, line := range lines {
		public = append(public, line.Public())
	}
	return ProxyLines{public, skipped}, nil
}
func (a *App) proxyPlatform(serial string) (string, error) {
	if err := a.proxySerial(serial); err != nil {
		return "", err
	}
	devices, err := a.GetDevices()
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.Serial == serial && d.State == "device" && (d.Platform == "android" || d.Platform == "ios") {
			return d.Platform, nil
		}
	}
	return "", errors.New("手机未连接或未授权，请检查设备连接")
}
func (a *App) proxyAPI(ctx context.Context, serial string) (*mks.API, error) {
	entries, err := a.usbmux.Devices(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		if d.Properties.Serial == serial {
			return mks.NewAPI(a.usbmux, d), nil
		}
	}
	return nil, errors.New("未找到对应 iPhone 的 USB 连接")
}
func (a *App) ChangeProxy(serial string, options proxyip.Options) (ProxyChange, error) {
	done, err := a.beginProxyOperation()
	if err != nil {
		return ProxyChange{}, err
	}
	defer done()
	platform, err := a.proxyPlatform(serial)
	if err != nil {
		return ProxyChange{}, err
	}
	a.proxy.mu.Lock()
	var line proxyip.Line
	for _, l := range a.proxy.lines {
		if l.ID == options.ProxyID {
			line = l
			break
		}
	}
	a.proxy.mu.Unlock()
	if line.ID == "" {
		return ProxyChange{}, errors.New("请选择有效代理，或刷新代理列表")
	}
	session, err := proxyip.NewSession()
	if err != nil {
		return ProxyChange{}, err
	}
	config, err := proxyip.Build(line, options, session)
	if err != nil {
		return ProxyChange{}, err
	}
	if err = a.RememberProxyOptions(serial, options); err != nil {
		return ProxyChange{}, fmt.Errorf("保存代理选择失败：%w", err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
	defer cancel()
	result := ProxyChange{ProxyName: line.Name, Region: config.Region}
	if platform == "ios" {
		api, err := a.proxyAPI(ctx, serial)
		if err != nil {
			return result, err
		}
		defer api.Close()
		if err = proxyip.ChangeIOS(ctx, api, config, proxyip.Wait); err != nil {
			return result, fmt.Errorf("切换未完成：%w。请先检查手机 VPN 状态，再重试", err)
		}
		result.Message = "已应用新代理会话，手机 VPN 已开启。可查询出口 IP 确认。"
		return result, nil
	}
	client, err := adb.Resolve()
	if err != nil {
		return result, err
	}
	// adb shell joins arguments: keep the nested credential-bearing URL one quoted argument.
	output, err := client.Command(ctx, "-s", serial, "shell", "am", "start", "-W", "-a", "android.intent.action.VIEW", "-d", proxyip.ShellQuote(config.ClashURL)).CombinedOutput()
	if err != nil || strings.Contains(string(output), "Error:") || strings.Contains(string(output), "Exception") || strings.Contains(string(output), "unable to resolve") {
		return result, errors.New("无法打开 Clash 配置导入页，请确认手机已安装支持 clash:// 的 Clash，并已解锁")
	}
	result.Pending = true
	result.Message = "已发送 Clash 配置导入链接。请在投屏画面保存配置，选中新配置并启用 Clash；完成后查询出口 IP。"
	return result, nil
}

func (a *App) QueryProxyIP(serial string) (proxyip.IPResult, error) {
	done, err := a.beginProxyOperation()
	if err != nil {
		return proxyip.IPResult{}, err
	}
	defer done()
	platform, err := a.proxyPlatform(serial)
	if err != nil {
		return proxyip.IPResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	command := proxyip.CurlQueryCommand("curl")
	var output string
	if platform == "ios" {
		api, e := a.proxyAPI(ctx, serial)
		if e != nil {
			return proxyip.IPResult{}, e
		}
		defer api.Close()
		output, err = api.JSONWithTimeout(ctx, "/runShell", map[string]string{"cmd": command}, 25*time.Second)
	} else {
		client, e := adb.Resolve()
		if e != nil {
			return proxyip.IPResult{}, e
		}
		curl, e := a.ensureAndroidCurl(ctx, client, serial)
		if e != nil {
			return proxyip.IPResult{}, e
		}
		b, e := client.Command(ctx, "-s", serial, "shell", proxyip.CurlQueryCommand(curl)).CombinedOutput()
		output = string(b)
		err = e
	}
	if err != nil {
		return proxyip.IPResult{}, errors.New("手机出口 IP 查询失败或超时，请检查手机网络和 curl")
	}
	output, err = proxyip.ShellOutput(output, true)
	if err != nil {
		if platform == "ios" && errors.Is(err, proxyip.ErrCurlMissing) {
			return proxyip.IPResult{MissingCurl: true}, nil
		}
		return proxyip.IPResult{}, err
	}
	return proxyip.ParseIP(output)
}

func (a *App) OpenIOSCurlStore(serial, store string) error {
	if _, err := proxyip.IOSCurlStoreURL(store); err != nil {
		return err
	}
	done, err := a.beginProxyOperation()
	if err != nil {
		return err
	}
	defer done()
	platform, err := a.proxyPlatform(serial)
	if err != nil {
		return err
	}
	if platform != "ios" {
		return errors.New("仅 iOS 手机支持跳转 Sileo 或 Cydia")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	api, err := a.proxyAPI(ctx, serial)
	if err != nil {
		return err
	}
	defer api.Close()
	return proxyip.OpenIOSCurlStore(ctx, api, store)
}

func (a *App) ensureAndroidCurl(ctx context.Context, client adb.Client, serial string) (string, error) {
	checksum := sha256.Sum256(androidCurl)
	run := func(command string) (string, error) { return client.Run(ctx, "-s", serial, "shell", command) }
	push := func(remote string) error {
		local, err := os.CreateTemp(a.tempDir, "android-curl-*")
		if err != nil {
			return err
		}
		name := local.Name()
		defer os.Remove(name)
		if _, err = local.Write(androidCurl); err != nil {
			_ = local.Close()
			return err
		}
		if err = local.Close(); err != nil {
			return err
		}
		_, err = client.Run(ctx, "-s", serial, "push", filepath.Clean(name), remote)
		return err
	}
	return proxyip.EnsureAndroidCurl(run, push, hex.EncodeToString(checksum[:]), randomID())
}
