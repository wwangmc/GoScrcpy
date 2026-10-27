package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux/muxtest"
)

type yoloTestSession struct {
	mirrorSession
	serial string
	done   chan struct{}
	once   sync.Once
}

func (s *yoloTestSession) SerialNumber() string         { return s.serial }
func (s *yoloTestSession) DoneChannel() <-chan struct{} { return s.done }
func (s *yoloTestSession) Close()                       { s.once.Do(func() { close(s.done) }) }

func TestYOLOConfig(t *testing.T) {
	for _, data := range []string{`{}`, `{"enabled":true,"port":0,"token":"0123456789abcdef"}`, `{"enabled":true,"port":8765,"token":"short"}`, `{"enabled":false,"port":8765,"token":"0123456789abcdef"}`, `{"enabled":0,"port":8765,"token":"0123456789abcdef"}`, `{"enabled":2,"port":8765,"token":"0123456789abcdef"}`} {
		if _, err := parseYOLOConfig(data); err == nil {
			t.Fatal("accepted invalid or disabled config")
		}
	}
	if c, err := parseYOLOConfig(`{"enabled":true,"port":9876,"token":"0123456789abcdef"}`); err != nil || c.Port != 9876 {
		t.Fatalf("config: %+v %v", c, err)
	}
	if c, err := parseYOLOConfig(`{"enabled":1,"port":8765,"token":"0123456789abcdef"}`); err != nil || !c.Enabled {
		t.Fatal("installed NSNumber config not accepted")
	}
}

func TestYOLOUSBBridgeLifecycle(t *testing.T) {
	const secret = "0123456789abcdef"
	upgrader := websocket.Upgrader{}
	connected, closed := make(chan struct{}, 4), make(chan struct{}, 4)
	yolo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/detections" || r.URL.Query().Get("token") != secret {
			t.Error("wrong PhoneYOLO handshake")
			http.Error(w, "unauthorized", 401)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		defer func() { closed <- struct{}{} }()
		connected <- struct{}{}
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"detections","detections":[]}`))
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer yolo.Close()
	mks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/runShell" || !strings.HasPrefix(body["cmd"], "cat /var/mobile/Library/PhoneYOLO/config.json ") {
			t.Error("unexpected phone command")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "data": `{"enabled":true,"port":9876,"token":"` + secret + `"}` + "\n__GOSCRCPY_PROXY_EXIT__:0\n"})
	}))
	defer mks.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {8081: strings.TrimPrefix(mks.URL, "http://"), 9876: strings.TrimPrefix(yolo.URL, "http://")}})
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.usbmux = mux.Client
	s := &yoloTestSession{serial: "USB-ONE", done: make(chan struct{})}
	a.session = s
	a.sessionID = "test-session"
	local, err := a.GetYOLOURL(s.serial)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(local, secret) {
		t.Fatal("phone token leaked")
	}
	if _, err = a.GetYOLOURL("USB-TWO"); err == nil {
		t.Fatal("other device accepted")
	}
	for _, query := range []string{"token=wrong", "token=" + a.token + "&session=old&serial=USB-ONE", "token=" + a.token + "&session=test-session&serial=USB-TWO"} {
		ws, response, err := websocket.DefaultDialer.Dial("ws://"+a.address+"/yolo?"+query, nil)
		if err == nil {
			ws.Close()
			t.Fatal("invalid local authentication accepted")
		}
		if response != nil {
			response.Body.Close()
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		ws, _, err := websocket.DefaultDialer.Dial(local, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil || !strings.Contains(string(data), `"detections"`) {
			t.Fatalf("bridge: %s %v", data, err)
		}
		select {
		case <-connected:
		case <-time.After(time.Second):
			t.Fatal("USB detection not connected")
		}
		if attempt == 0 {
			ws.Close()
		} else {
			a.StopMirror()
			if _, _, err = ws.ReadMessage(); err == nil {
				t.Fatal("mirror stop kept detection open")
			}
			ws.Close()
		}
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("PhoneYOLO connection leaked")
		}
	}
	parsed, _ := url.Parse(local)
	parsed.RawQuery = "token=" + a.token + "&session=test-session&serial=USB-ONE"
	if ws, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil); err == nil {
		ws.Close()
		t.Fatal("stopped mirror accepted")
	}
}

// Opt-in real-device check: only reads video and detections, sends no input.
func TestYOLODeviceStream(t *testing.T) {
	serial := os.Getenv("YOLO_TEST_DEVICE")
	if serial == "" {
		t.Skip("set YOLO_TEST_DEVICE (or auto) for live USB detection verification")
	}
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	if serial == "auto" {
		devices, err := a.GetDevices()
		if err != nil {
			t.Fatal(err)
		}
		serial = ""
		for _, device := range devices {
			if device.Platform == "ios" && device.State == "device" {
				if serial != "" {
					t.Fatal("multiple iPhones; specify YOLO_TEST_DEVICE")
				}
				serial = device.Serial
			}
		}
		if serial == "" {
			t.Fatal("no ready iPhone")
		}
	}
	mirror, err := a.StartMirror(serial, scrcpy.Options{VideoMode: "jpeg", MaxFPS: 15, MaxSize: 1920, BitRate: 8000000})
	if err != nil {
		t.Fatal(err)
	}
	video, _, err := websocket.DefaultDialer.Dial(mirror.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	_ = video.SetReadDeadline(time.Now().Add(6 * time.Second))
	if _, _, err = video.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	local, err := a.GetYOLOURL(serial)
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := websocket.DefaultDialer.Dial(local, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var first, last int
	var targets int
	var elapsed float64
	for i := 0; i < 15; i++ {
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Type, Message string
			Sequence      int
			Latency       float64 `json:"latencyMs"`
			Screen        struct{ Width, Height int }
			Detections    []json.RawMessage
		}
		if json.Unmarshal(data, &result) != nil || result.Type != "detections" {
			t.Fatalf("YOLO response type=%s message=%s", result.Type, result.Message)
		}
		if result.Screen.Width <= 0 || result.Screen.Height <= 0 || i > 0 && result.Sequence <= last {
			t.Fatal("invalid detection dimensions or sequence")
		}
		if i == 0 {
			first = result.Sequence
		}
		last = result.Sequence
		targets += len(result.Detections)
		elapsed += result.Latency
	}
	a.StopMirror()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err = ws.ReadMessage(); err != nil {
			break
		}
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("detection did not close after mirror stopped")
	}
	t.Logf("live USB: video=%s %dx%d, detection frames=15 sequence=%d..%d total targets=%d mean latency=%.1f ms; stop closed detection", mirror.Codec, mirror.Metadata.Width, mirror.Metadata.Height, first, last, targets, elapsed/15)
}
