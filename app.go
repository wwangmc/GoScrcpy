package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"phonescreen/internal/adb"
	"phonescreen/internal/device"
	"phonescreen/internal/phonevideo"
	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux"
)

type App struct {
	iosSettingsMu       sync.Mutex
	files               fileService
	ctx                 context.Context
	cancel              context.CancelFunc
	mu                  sync.Mutex
	session             mirrorSession
	usbmux              *usbmux.Client
	sessionID           string
	lastError           string
	serverPath, tempDir string
	token, address      string
	httpServer          *http.Server
	mux                 *http.ServeMux
	once                sync.Once
	smokeSerial         string
	smokeOnce           sync.Once
	wailsContext        context.Context
	mirrorSerial        string
	fileSerial          string
	preferenceRoot      string
	launchSettings      scrcpy.Options
	windowsMu           sync.Mutex
	windows             map[string]bool
}

type LaunchOptions struct {
	LastDevice   string         `json:"lastDevice"`
	FileSerial   string         `json:"fileSerial"`
	SmokeSerial  string         `json:"smokeSerial"`
	MirrorSerial string         `json:"mirrorSerial"`
	Settings     scrcpy.Options `json:"settings"`
}

type SmokeReport struct {
	Codec  string `json:"codec"`
	Frames int    `json:"frames"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Error  string `json:"error"`
}

type State struct {
	ADBPath string `json:"adbPath"`
	Error   string `json:"error"`
	Serial  string `json:"serial"`
	Active  bool   `json:"active"`
	Logs    string `json:"logs"`
	Version string `json:"version"`
}

type Mirror struct {
	URL               string          `json:"url"`
	Metadata          scrcpy.Metadata `json:"metadata"`
	Codec             string          `json:"codec"`
	VideoModes        []string        `json:"videoModes"`
	Controls          []string        `json:"controls"`
	Settings          []string        `json:"settings"`
	PointerIntervalMS int             `json:"pointerIntervalMS"`
}

func randomID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func NewApp(server []byte) (*App, error) {
	dir, err := os.MkdirTemp("", "goscrcpy-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "scrcpy-server-v"+scrcpy.Version)
	if err = os.WriteFile(path, server, 0600); err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{ctx: ctx, cancel: cancel, serverPath: path, tempDir: dir, token: randomID(), mux: http.NewServeMux(), usbmux: usbmux.NewClient()}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		_ = os.Remove(path)
		_ = os.Remove(dir)
		return nil, err
	}
	a.address = listener.Addr().String()
	a.mux.HandleFunc("/stream", a.stream)
	a.httpServer = &http.Server{Handler: a.mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = a.httpServer.Serve(listener) }()
	return a, nil
}

func (a *App) startup(ctx context.Context) {
	a.wailsContext = ctx
	if a.smokeSerial != "" {
		go func() {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(40 * time.Second):
				a.ReportSmoke(SmokeReport{Error: "桌面自检超时，前端未完成解码"})
			}
		}()
	}
}

func (a *App) GetLaunchOptions() LaunchOptions {
	return LaunchOptions{SmokeSerial: a.smokeSerial, MirrorSerial: a.mirrorSerial, FileSerial: a.fileSerial, LastDevice: a.lastDevice(), Settings: a.launchSettings}
}

// ReportSmoke is used only by the explicitly selected native self-test mode.
// Frames are counted after the actual WebView2 decoder output has been drawn.
func (a *App) ReportSmoke(report SmokeReport) {
	if a.smokeSerial == "" {
		return
	}
	a.smokeOnce.Do(func() {
		state := a.GetState()
		if report.Error == "" && (!state.Active || report.Frames < 1 || report.Width <= 0 || report.Height <= 0) {
			report.Error = "未收到有效 WebView2 解码画面或会话已经关闭"
		}
		if mode := a.launchSettings.VideoMode; report.Error == "" && (mode == "jpeg" || mode == "h264") && report.Codec != mode {
			report.Error = "原生窗口没有使用指定的投屏方式"
		}
		a.StopMirror()
		if report.Error != "" {
			log.Printf("NATIVE_SMOKE_FAIL: %s; server=%s", report.Error, state.Logs)
		} else {
			log.Printf("NATIVE_SMOKE_PASS: device=%s codec=%s decoded_frames=%d video=%dx%d; Wails binding, USB video, WebView2 rendering and stop completed", a.smokeSerial, report.Codec, report.Frames, report.Width, report.Height)
		}
		if a.wailsContext != nil {
			wailsruntime.Quit(a.wailsContext)
		}
	})
}
func (a *App) shutdown(_ context.Context) { a.close() }
func (a *App) close() {
	a.once.Do(func() {
		a.cancel()
		a.CancelFileTransfer()
		a.files.ops.Lock()
		a.files.ios.Close()
		a.files.ops.Unlock()
		a.mu.Lock()
		if a.session != nil {
			a.session.Close()
		}
		a.mu.Unlock()
		_ = a.httpServer.Close()
		_ = os.Remove(a.serverPath)
		_ = os.Remove(a.tempDir)
	})
}

func (a *App) GetDevices() ([]device.Device, error) {
	var android, ios []device.Device
	var androidErr, iosErr error
	var scans sync.WaitGroup
	scans.Add(2)
	go func() {
		defer scans.Done()
		c, err := adb.Resolve()
		if err != nil {
			androidErr = err
			return
		}
		android, androidErr = c.Devices(a.ctx)
	}()
	go func() {
		defer scans.Done()
		ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
		defer cancel()
		ios, iosErr = phonevideo.Scan(ctx, a.usbmux)
	}()
	scans.Wait()
	devices := append(make([]device.Device, 0, len(android)+len(ios)), android...)
	devices = append(devices, ios...)
	if androidErr != nil && iosErr != nil {
		return nil, fmt.Errorf("设备检测失败：%v；%v", androidErr, iosErr)
	}
	return devices, nil
}

func (a *App) GetState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := State{Version: scrcpy.Version, Error: a.lastError}
	c, err := adb.Resolve()
	if err != nil {
		// ADB is optional when only USBMux devices are connected.
	} else {
		s.ADBPath = c.Path
	}
	if a.session != nil {
		s.Serial, s.Logs = a.session.SerialNumber(), a.session.Logs()
		select {
		case <-a.session.DoneChannel():
		default:
			s.Active = true
		}
	}
	return s
}

func (a *App) StartMirror(serial string, options scrcpy.Options) (Mirror, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	devices, err := a.GetDevices()
	if err != nil {
		return Mirror{}, err
	}
	found := false
	platform := ""
	var videoModes []string
	for _, d := range devices {
		if d.Serial == serial {
			found = true
			platform = d.Platform
			videoModes = d.VideoModes
			if d.State != "device" {
				if d.Error != "" {
					return Mirror{}, fmt.Errorf("%s", d.Error)
				}
				return Mirror{}, fmt.Errorf("设备尚未就绪: %s，请检查手机授权和连接", d.State)
			}
			break
		}
	}
	if !found {
		return Mirror{}, fmt.Errorf("设备已断开，请刷新设备列表")
	}
	if err = options.Validate(); err != nil {
		return Mirror{}, err
	}
	if platform != "ios" {
		if options.VideoMode == "jpeg" {
			return Mirror{}, fmt.Errorf("当前设备仅支持 USB H.264")
		}
		videoModes = []string{"h264"}
	}
	if a.session != nil {
		a.session.Close()
		a.session = nil
	}
	a.lastError = ""
	var session mirrorSession
	if platform == "ios" {
		session, err = phonevideo.Start(a.ctx, a.usbmux, serial, options)
	} else {
		var client adb.Client
		client, err = adb.Resolve()
		if err == nil {
			session, err = scrcpy.Start(a.ctx, client, a.serverPath, serial, options)
		}
	}
	if err != nil {
		a.lastError = err.Error()
		return Mirror{}, err
	}
	a.session, a.sessionID = session, randomID()
	// A failed frontend attachment must not leave the encoder running indefinitely.
	go func() {
		select {
		case <-session.DoneChannel():
			return
		case <-time.After(15 * time.Second):
		}
		if session.Claim() {
			session.Close()
		}
	}()
	return Mirror{URL: "ws://" + a.address + "/stream?token=" + a.token + "&id=" + a.sessionID, Metadata: session.Info(), Codec: session.Encoding(), VideoModes: videoModes, Controls: session.Controls(), Settings: session.Settings(), PointerIntervalMS: session.PointerIntervalMS()}, nil
}

func (a *App) StopMirror() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
		a.session = nil
	}
}

func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != a.token {
		http.Error(w, "forbidden", 403)
		return
	}
	a.mu.Lock()
	s, id := a.session, a.sessionID
	a.mu.Unlock()
	if s == nil || id != r.URL.Query().Get("id") {
		http.Error(w, "session expired", 410)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }, WriteBufferSize: 64 * 1024}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if !s.Claim() {
		_ = ws.Close()
		return
	}
	defer ws.Close()
	defer s.Close()
	ws.SetReadLimit(65536)
	go func() {
		defer s.Close()
		for {
			kind, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.BinaryMessage || s.WriteControl(data) != nil {
				return
			}
		}
	}()
	for {
		packet, err := s.ReadFrame()
		if err != nil {
			a.mu.Lock()
			if a.session == s {
				a.lastError = "投屏连接已结束。" + s.Logs()
			}
			a.mu.Unlock()
			return
		}
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err = ws.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			return
		}
	}
}

// Browser mode uses the same backend as Wails and is useful for integration diagnostics.
func (a *App) webURL() string { return "http://" + a.address + "/?token=" + a.token }
func (a *App) enableWeb(files fs.FS) {
	a.mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+a.token || r.Method != http.MethodPost {
			http.Error(w, "forbidden", 403)
			return
		}
		var request struct {
			Args      []string       `json:"args"`
			Method    string         `json:"method"`
			Serial    string         `json:"serial"`
			Options   scrcpy.Options `json:"options"`
			Action    string         `json:"action"`
			Clipboard string         `json:"clipboard"`
			Store     string         `json:"store"`
			Region    string         `json:"region"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		var result any
		var err error
		switch request.Method {
		case "GetDevices":
			result, err = a.GetDevices()
		case "GetState":
			result = a.GetState()
		case "GetLaunchOptions":
			result = a.GetLaunchOptions()
		case "OpenDevice":
			err = a.OpenDevice(request.Serial, request.Options)
		case "GetOpenDevices":
			result = a.GetOpenDevices()
		case "DeviceTool":
			result, err = a.DeviceTool(request.Serial, request.Action, request.Clipboard)
		case "OpenShell":
			result, err = a.OpenShell(request.Serial)
		case "OpenOpenSSHStore":
			err = a.OpenOpenSSHStore(request.Serial, request.Store)
		case "GetIOSSettings":
			result, err = a.GetIOSSettings(request.Serial)
		case "RememberIOSSettingsRegion":
			err = a.RememberIOSSettingsRegion(request.Serial, request.Region)
		case "ApplyIOSSettings":
			result, err = a.ApplyIOSSettings(request.Serial, request.Region)
		case "OpenFileManager", "BrowseFiles", "FileSharedApps", "CreateFileDirectory", "RenameFile", "DeleteFile", "PasteFile", "UploadFile", "DownloadFile", "GetFileTransfer", "CancelFileTransfer", "GetFileLocation", "RememberDevice":
			result, err = a.fileRequest(request.Method, request.Args)
		case "StartMirror":
			result, err = a.StartMirror(request.Serial, request.Options)
		case "StopMirror":
			a.StopMirror()
		default:
			http.Error(w, "unknown method", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	a.mux.Handle("/", http.FileServer(http.FS(files)))
}
