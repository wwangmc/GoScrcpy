// Package mks consumes mks_tweak's existing API exclusively through USBMux.
package mks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"phonescreen/internal/device"
	"phonescreen/internal/usbmux"
)

const Port uint16 = 8081 // Device port only. The computer never listens on 8081.

type API struct {
	client    *http.Client
	transport *http.Transport
	dialUSB   func(context.Context) (net.Conn, error)
}

type timedConn struct{ net.Conn }

func (c timedConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	return c.Conn.Read(b)
}
func (c timedConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(4 * time.Second))
	return c.Conn.Write(b)
}

func NewAPI(mux *usbmux.Client, d usbmux.Device) *API {
	dialUSB := func(ctx context.Context) (net.Conn, error) { return mux.Connect(ctx, d, Port) }
	t := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialUSB(ctx)
			if err != nil {
				return nil, err
			}
			return timedConn{conn}, nil
		},
		MaxConnsPerHost: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 10 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
	}
	return &API{client: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport: t, dialUSB: dialUSB}
}

func (a *API) ConnectControl(ctx context.Context) (*websocket.Conn, error) {
	dialer := websocket.Dialer{NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return a.dialUSB(ctx) }, HandshakeTimeout: 2 * time.Second, WriteBufferSize: 1024, ReadBufferSize: 1024}
	conn, response, err := dialer.DialContext(ctx, "ws://mks.local:8081/", nil)
	if err != nil && response != nil {
		_ = response.Body.Close()
	}
	return conn, err
}
func (a *API) Close() { a.transport.CloseIdleConnections() }

func (a *API) request(ctx context.Context, path string, body any) (*http.Response, error) {
	method := http.MethodGet
	var input io.Reader
	if body != nil {
		method = http.MethodPost
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://mks.local:8081"+path, input)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Cache-Control", "no-cache")
	r, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("手机服务 %s: %w", path, err)
	}
	if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusNoContent {
		defer r.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(r.Body, 512))
		return nil, &statusError{r.StatusCode, fmt.Sprintf("手机服务 %s 返回 HTTP %d: %s", path, r.StatusCode, strings.TrimSpace(string(b)))}
	}
	return r, nil
}

type statusError struct {
	code    int
	message string
}

func (e *statusError) Error() string { return e.message }

func (a *API) JSON(ctx context.Context, path string, body any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	r, err := a.request(ctx, path, body)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	var result struct {
		Status string `json:"status"`
		Data   string `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&result); err != nil {
		return "", fmt.Errorf("手机接口 %s 的响应不是有效 JSON: %w", path, err)
	}
	if result.Status != "success" {
		return "", fmt.Errorf("手机接口 %s 执行失败: %s", path, result.Status)
	}
	return result.Data, nil
}

func (a *API) ScreenSize(ctx context.Context) (int, int, error) {
	data, err := a.JSON(ctx, "/deviceInfo?type=1", nil)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(data, ";;")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无效手机屏幕尺寸: %q", data)
	}
	w, e1 := strconv.ParseFloat(parts[0], 64)
	h, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || !(w >= 1 && w <= 16384 && h >= 1 && h <= 16384) {
		return 0, 0, fmt.Errorf("无效手机屏幕尺寸: %q", data)
	}
	return int(w), int(h), nil
}

func Scan(ctx context.Context, mux *usbmux.Client) ([]device.Device, error) {
	entries, err := mux.Devices(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]device.Device, len(entries))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, d := range entries {
		workers.Add(1)
		go func(i int, d usbmux.Device) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()
			item := device.Device{Serial: d.Properties.Serial, State: "unavailable", Model: "iPhone", Transport: "USB", Platform: "ios", Codec: "jpeg", Controls: supportedControls(), Settings: []string{"maxFPS"}}
			api := NewAPI(mux, d)
			defer api.Close()
			probe, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
			defer cancel()
			info, err := api.JSON(probe, "/deviceInfo?type=30", nil)
			if err != nil {
				item.Error = "USB 已连接，无法访问手机服务，请确认 mks_tweak 正在监听手机端 8081。" + err.Error()
			} else {
				parts := strings.Split(info, ";;")
				if len(parts) < 4 || parts[0] == "" {
					item.Error = "手机设备信息响应不完整"
				} else {
					item.Model = parts[0]
					item.Product = parts[3]
					item.State = "device"
				}
			}
			result[i] = item
		}(i, d)
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
