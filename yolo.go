package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"phonescreen/internal/mks"
	"phonescreen/internal/proxyip"
)

type yoloConfig struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
}

var yoloToken = regexp.MustCompile(`^[a-zA-Z0-9]{16,128}$`)

func parseYOLOConfig(data string) (yoloConfig, error) {
	var c yoloConfig
	var raw struct {
		Enabled json.RawMessage `json:"enabled"`
		Port    int             `json:"port"`
		Token   string          `json:"token"`
	}
	err := json.Unmarshal([]byte(data), &raw)
	c.Port, c.Token = raw.Port, raw.Token
	// Some installed builds serialize Objective-C NSNumber BOOL as 0/1.
	enabled := strings.TrimSpace(string(raw.Enabled))
	c.Enabled = enabled == "true" || enabled == "1"
	if err != nil || enabled != "true" && enabled != "false" && enabled != "0" && enabled != "1" || c.Port < 1024 || c.Port > 65535 || !yoloToken.MatchString(c.Token) {
		return c, errors.New("未能读取 PhoneYOLO 配置，请在手机打开 PhoneYOLO 并保存配置")
	}
	if !c.Enabled {
		return c, errors.New("PhoneYOLO 服务已关闭，请先在手机启用服务")
	}
	return c, nil
}

// GetYOLOURL returns only a local, session-bound URL. Phone credentials stay in Go.
func (a *App) GetYOLOURL(serial string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if serial == "" || a.fileSerial != "" || a.mirrorSerial != "" && a.mirrorSerial != serial || a.session == nil || a.session.SerialNumber() != serial {
		return "", errors.New("请先连接对应 iPhone 的投屏")
	}
	select {
	case <-a.session.DoneChannel():
		return "", errors.New("投屏已断开，请重新连接")
	default:
	}
	return "ws://" + a.address + "/yolo?" + url.Values{"token": {a.token}, "session": {a.sessionID}, "serial": {serial}}.Encode(), nil
}

func (a *App) yoloStream(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != a.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	a.mu.Lock()
	s := a.session
	valid := s != nil && r.URL.Query().Get("session") == a.sessionID && r.URL.Query().Get("serial") == s.SerialNumber()
	a.mu.Unlock()
	if !valid {
		http.Error(w, "inactive mirror", http.StatusConflict)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stopClose()
	ws.SetReadLimit(1024)
	go func() {
		defer cancel()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	go func() {
		select {
		case <-s.DoneChannel():
			cancel()
		case <-ctx.Done():
		}
	}()
	fail := func(message string) {
		_ = ws.SetWriteDeadline(time.Now().Add(3 * time.Second))
		_ = ws.WriteJSON(map[string]any{"type": "error", "message": message, "fatal": true})
	}
	setup, setupCancel := context.WithTimeout(ctx, 8*time.Second)
	defer setupCancel()
	device, err := usbDevice(setup, a.usbmux, s.SerialNumber())
	if err != nil {
		fail("未找到 iPhone USB 连接，请检查数据线")
		return
	}
	api := mks.NewAPI(a.usbmux, device)
	defer api.Close()
	output, err := api.JSON(setup, "/runShell", map[string]string{"cmd": proxyip.ShellCommand("cat /var/mobile/Library/PhoneYOLO/config.json")})
	if err == nil {
		output, err = proxyip.ShellOutput(output, true)
	}
	if err != nil {
		// Shell and dial errors may contain secrets; never forward raw errors.
		fail("无法读取 PhoneYOLO 配置，请确认已安装 PhoneYOLO、启用服务，且 MKS 可读取配置")
		return
	}
	config, err := parseYOLOConfig(output)
	if err != nil {
		fail(err.Error())
		return
	}
	dialer := websocket.Dialer{HandshakeTimeout: 4 * time.Second, NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return a.usbmux.Connect(ctx, device, uint16(config.Port))
	}}
	remoteURL := fmt.Sprintf("ws://phoneyolo.local:%d/detections?token=%s", config.Port, url.QueryEscape(config.Token))
	remote, response, err := dialer.DialContext(setup, remoteURL, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		fail("无法连接 PhoneYOLO，请在手机确认服务已启用；保存配置后可重新开启 YOLO 模式")
		return
	}
	defer remote.Close()
	setupCancel()
	stopRemote := context.AfterFunc(ctx, func() { _ = remote.Close() })
	defer stopRemote()
	remote.SetReadLimit(1024 * 1024)
	for {
		kind, data, err := remote.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage || !json.Valid(data) {
			continue
		}
		_ = ws.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if ws.WriteMessage(websocket.TextMessage, data) != nil {
			return
		}
	}
}
