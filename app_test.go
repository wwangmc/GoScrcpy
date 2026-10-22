package main

import (
	"context"
	"encoding/binary"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"phonescreen/internal/adb"
	"phonescreen/internal/scrcpy"
)

func TestLocalStreamRequiresToken(t *testing.T) {
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	r, err := http.Get("http://" + a.address + "/stream?token=invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthenticated stream: %d", r.StatusCode)
	}
	if _, err = a.StartMirror("unknown-device", scrcpy.Options{MaxSize: 1920, MaxFPS: 60, BitRate: 8000000}); err == nil {
		t.Fatal("unknown device accepted")
	}
}

func TestDeviceWebSocketLifecycle(t *testing.T) {
	serial := os.Getenv("SCRCPY_TEST_DEVICE")
	if serial == "" {
		t.Skip("set SCRCPY_TEST_DEVICE for the real-device integration test")
	}
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	client, err := adb.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		initialForwards, err := client.Run(context.Background(), "forward", "--list")
		if err != nil {
			t.Fatal(err)
		}
		mirror, err := a.StartMirror(serial, scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000})
		if err != nil {
			t.Fatal(err)
		}
		// Record only this session's forwarding entry; other scrcpy clients may coexist.
		forwardBefore, err := client.Run(context.Background(), "forward", "--list")
		if err != nil {
			t.Fatal(err)
		}
		var ownForward string
		for _, line := range strings.Split(forwardBefore, "\n") {
			if strings.HasPrefix(line, serial+" ") && strings.Contains(line, "localabstract:scrcpy_") && !strings.Contains(initialForwards, line) {
				ownForward = line
			}
		}
		if ownForward == "" {
			t.Fatal("session forwarding entry was not created")
		}
		ws, _, err := websocket.DefaultDialer.Dial(mirror.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		kind, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind != websocket.BinaryMessage || len(data) < 13 || data[0]&0x80 == 0 || int(binary.BigEndian.Uint32(data[8:])) != len(data)-12 {
			t.Fatalf("invalid initial config: %x", data)
		}
		key := make([]byte, 14)
		binary.BigEndian.PutUint32(key[2:], 3)
		if err = ws.WriteMessage(websocket.BinaryMessage, key); err != nil {
			t.Fatal(err)
		}
		key[1] = 1
		if err = ws.WriteMessage(websocket.BinaryMessage, key); err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			_ = ws.Close()
		} else {
			a.StopMirror()
			_ = ws.Close()
		}
		deadline := time.Now().Add(5 * time.Second)
		for a.GetState().Active && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if a.GetState().Active {
			t.Fatal("session remained active after disconnect/stop")
		}
		a.StopMirror()
		forward, err := client.Run(context.Background(), "forward", "--list")
		if err != nil {
			t.Fatal(err)
		}
		if ownForward != "" && strings.Contains(forward, ownForward) {
			t.Fatalf("scrcpy forward leaked after attempt %d: %s", attempt, forward)
		}
		t.Logf("attempt %d: %dx%d, WebSocket H264 packet, control, disconnect/stop cleanup OK", attempt, mirror.Metadata.Width, mirror.Metadata.Height)
	}
}
