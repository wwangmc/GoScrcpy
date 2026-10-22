package main

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"phonescreen/internal/scrcpy"
)

func TestIOSDeviceWebSocketLifecycle(t *testing.T) {
	serial := os.Getenv("MKS_TEST_DEVICE")
	if serial == "" {
		t.Skip("set MKS_TEST_DEVICE for the USB MKS integration test")
	}
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	devices, err := a.GetDevices()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range devices {
		if d.Serial == serial && d.Platform == "ios" && d.Transport == "USB" && d.State == "device" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ready device missing from unified device list: %+v", devices)
	}
	for attempt := 0; attempt < 2; attempt++ {
		mirror, err := a.StartMirror(serial, scrcpy.Options{MaxSize: 1920, MaxFPS: 30, BitRate: 8000000})
		if err != nil {
			t.Fatal(err)
		}
		if mirror.Codec != "jpeg" || !slices.Contains(mirror.Controls, "home") || slices.Contains(mirror.Controls, "volumeUp") {
			t.Fatalf("unexpected capabilities: %+v", mirror)
		}
		ws, _, err := websocket.DefaultDialer.Dial(mirror.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ws.SetReadDeadline(time.Now().Add(6 * time.Second))
		// A static phone legitimately returns 204 after the first frame.
		for i := 0; i < 1; i++ {
			kind, packet, err := ws.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if kind != websocket.BinaryMessage || len(packet) < 13 || int(binary.BigEndian.Uint32(packet[8:])) != len(packet)-12 {
				t.Fatal("invalid image frame")
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(packet[12:]))
			if err != nil || config.Width <= 0 || config.Height <= 0 {
				t.Fatalf("image=%+v err=%v", config, err)
			}
		}
		key := make([]byte, 14)
		binary.BigEndian.PutUint32(key[2:], 3)
		if err = ws.WriteMessage(websocket.BinaryMessage, key); err != nil {
			t.Fatal(err)
		}
		key[1] = 1
		_ = ws.WriteMessage(websocket.BinaryMessage, key)
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
			t.Fatal("USB session leaked after stop/disconnect")
		}
		a.StopMirror()
		t.Logf("attempt %d: unified list, USB MKS JPEG %dx%d, input, reconnect, close OK", attempt, mirror.Metadata.Width, mirror.Metadata.Height)
	}
}
