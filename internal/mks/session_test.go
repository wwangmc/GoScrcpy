package mks

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux/muxtest"
)

type input struct {
	Path string
	Body map[string]any
}
type fakePhone struct {
	mu         sync.Mutex
	calls      []input
	streaming  atomic.Int32
	fallback   bool
	rejectDown bool
	disableWS  bool
	polls      atomic.Int32
	originals  atomic.Int32
	streams    atomic.Int32
	quality    string
}

func jpegData() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{20, 180, 80, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}
func (p *fakePhone) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		if p.disableWS {
			http.NotFound(w, r)
			return
		}
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			body := map[string]any{}
			if msg.Type == "touch" {
				_ = json.Unmarshal(msg.Data, &body)
			} else if msg.Type == "inputText" {
				var text string
				_ = json.Unmarshal(msg.Data, &text)
				body["text"] = text
			} else {
				var num float64
				_ = json.Unmarshal(msg.Data, &num)
				body["num"] = num
			}
			p.mu.Lock()
			p.calls = append(p.calls, input{"ws:" + msg.Type, body})
			p.mu.Unlock()
		}
	case "/deviceInfo":
		data := "Phone;;iOS;;15.6;;iPhone11,8;;id"
		if r.URL.Query().Get("type") == "1" {
			data = "828.000000;;1792.000000"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "data": data})
	case "/screenshotStream":
		p.streams.Add(1)
		if p.fallback {
			http.NotFound(w, r)
			return
		}
		p.streaming.Add(1)
		defer p.streaming.Add(-1)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=--frame")
		b := jpegData()
		for {
			_, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(b))
			if err != nil {
				return
			}
			if _, err = w.Write(b); err != nil {
				return
			}
			_, _ = io.WriteString(w, "\r\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	case "/screenshot":
		p.mu.Lock()
		p.quality = r.URL.Query().Get("quality")
		p.mu.Unlock()
		if p.polls.Add(1) <= 2 {
			w.WriteHeader(204)
			return
		}
		fallthrough
	case "/screenshot_v2", "/screenshot_original":
		if r.URL.Path == "/screenshot_original" {
			p.originals.Add(1)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegData())
	default:
		body := map[string]any{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		p.mu.Lock()
		p.calls = append(p.calls, input{r.URL.RequestURI(), body})
		p.mu.Unlock()
		if p.rejectDown && r.URL.Path == "/touch" && body["type"] == "down" {
			http.Error(w, "acknowledgement lost", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}
}

func TestMKSUSBSessionAndControls(t *testing.T) {
	// Occupying local 8081 cannot affect any phone connection. An existing listener is fine too.
	if occupied, err := net.Listen("tcp", "127.0.0.1:8081"); err == nil {
		defer occupied.Close()
	}
	phone := &fakePhone{}
	httpPhone := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer httpPhone.Close()
	mux := muxtest.Start(t, map[uint32]string{1: strings.TrimPrefix(httpPhone.URL, "http://"), 2: strings.TrimPrefix(httpPhone.URL, "http://")})
	devices, err := Scan(context.Background(), mux.Client)
	if err != nil || len(devices) != 2 {
		t.Fatalf("scan=%+v err=%v", devices, err)
	}
	for _, d := range devices {
		if d.State != "device" || d.Transport != "USB" {
			t.Fatalf("not ready: %+v", d)
		}
	}
	s, err := Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1920, MaxFPS: 60, BitRate: 8000000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Encoding() != "jpeg" || s.Info().Width != 8 || s.Info().Height != 12 || !s.Claim() || s.Claim() {
		t.Fatal("metadata, format, or session claim incorrect")
	}
	frame, err := s.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if int(binary.BigEndian.Uint32(frame[8:])) != len(frame)-12 {
		t.Fatal("wrong image frame length")
	}
	if _, err = jpeg.Decode(bytes.NewReader(frame[12:])); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 14)
	binary.BigEndian.PutUint32(key[2:], 3)
	if err = s.WriteControl(key); err != nil {
		t.Fatal(err)
	}
	key[1] = 1
	if err = s.WriteControl(key); err != nil {
		t.Fatal(err)
	}
	text := []byte("你好 USB")
	textPacket := make([]byte, 14+len(text))
	textPacket[0] = 8
	binary.BigEndian.PutUint32(textPacket[10:], uint32(len(text)))
	copy(textPacket[14:], text)
	if err = s.WriteControl(textPacket); err != nil {
		t.Fatal(err)
	}
	touch := make([]byte, 32)
	touch[0] = 2
	binary.BigEndian.PutUint32(touch[10:], 4)
	binary.BigEndian.PutUint32(touch[14:], 6)
	binary.BigEndian.PutUint16(touch[18:], 8)
	binary.BigEndian.PutUint16(touch[20:], 12)
	if err = s.WriteControl(touch); err != nil {
		t.Fatal(err)
	}
	touch[1] = 2
	if err = s.WriteControl(touch); err != nil {
		t.Fatal(err)
	}
	s.Close() // A held finger must be released before the USB channel is torn down.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pCount := 0
		phone.mu.Lock()
		pCount = len(phone.calls)
		phone.mu.Unlock()
		if pCount >= 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	phone.mu.Lock()
	calls := append([]input{}, phone.calls...)
	phone.mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if calls[0].Path != "/pressHome" || calls[1].Path != "ws:inputText" || calls[1].Body["text"] != "你好 USB" {
		t.Fatalf("wrong key/text mapping: %+v", calls)
	}
	for i, action := range []string{"down", "move", "up"} {
		c := calls[i+2]
		if c.Path != "ws:touch" || c.Body["type"] != action || c.Body["x"] != float64(414) || c.Body["y"] != float64(896) {
			t.Fatalf("wrong physical touch mapping: %+v", c)
		}
	}
	deadline = time.Now().Add(time.Second)
	for (mux.Active.Load() != 0 || phone.streaming.Load() != 0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if mux.Active.Load() != 0 || phone.streaming.Load() != 0 {
		t.Fatalf("USB/stream leaked: %d/%d", mux.Active.Load(), phone.streaming.Load())
	}
	if err = s.WriteControl(touch); err == nil {
		t.Fatal("closed session accepted input")
	}
	if phone.streams.Load() != 0 || phone.quality != "0.1" {
		t.Fatal("slow stream/high-quality path was selected")
	}
}

func TestScreenshotFirstFrameAndNoChange(t *testing.T) {
	phone := &fakePhone{fallback: true}
	httpPhone := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer httpPhone.Close()
	mux := muxtest.Start(t, map[uint32]string{1: strings.TrimPrefix(httpPhone.URL, "http://")})
	s, err := Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1280, MaxFPS: 60, BitRate: 4000000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		frame, err := s.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = jpeg.Decode(bytes.NewReader(frame[12:])); err != nil {
			t.Fatal("204 was incorrectly emitted as a frame")
		}
	}
	if phone.originals.Load() != 1 || phone.polls.Load() < 3 || phone.streams.Load() != 0 {
		t.Fatal("first-frame snapshot/no-change retry did not run, or the slow stream was used")
	}
}

func TestFailedTouchAcknowledgementStillReleasesFinger(t *testing.T) {
	phone := &fakePhone{rejectDown: true, disableWS: true}
	server := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer server.Close()
	mux := muxtest.Start(t, map[uint32]string{1: strings.TrimPrefix(server.URL, "http://")})
	s, err := Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000})
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 32)
	packet[0] = 2
	binary.BigEndian.PutUint16(packet[18:], 8)
	binary.BigEndian.PutUint16(packet[20:], 12)
	if err = s.WriteControl(packet); err == nil {
		t.Fatal("failed HTTP acknowledgement was accepted")
	}
	s.Close()
	phone.mu.Lock()
	defer phone.mu.Unlock()
	if len(phone.calls) != 2 || phone.calls[1].Body["type"] != "up" {
		t.Fatalf("touch was not released: %+v", phone.calls)
	}
}
