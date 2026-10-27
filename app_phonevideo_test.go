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
	serial := os.Getenv("PHONEVIDEO_TEST_DEVICE")
	if serial == "" {
		t.Skip("set PHONEVIDEO_TEST_DEVICE for the USB PhoneVideo integration test")
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
		if (mirror.Codec != "jpeg" && mirror.Codec != "h264") || !slices.Contains(mirror.Controls, "home") || slices.Contains(mirror.Controls, "volumeUp") {
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
			if mirror.Codec == "jpeg" {
				config, _, err := image.DecodeConfig(bytes.NewReader(packet[12:]))
				if err != nil || config.Width <= 0 || config.Height <= 0 {
					t.Fatalf("image=%+v err=%v", config, err)
				}
			} else if binary.BigEndian.Uint64(packet)&(uint64(1)<<63) == 0 {
				t.Fatal("H.264 stream must begin with SPS/PPS configuration")
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
		t.Logf("attempt %d: unified list, USB PhoneVideo %s %dx%d, input, reconnect, close OK", attempt, mirror.Codec, mirror.Metadata.Width, mirror.Metadata.Height)
	}
}

func TestIOSVideoModeSwitch(t *testing.T) {
	serial := os.Getenv("PHONEVIDEO_TEST_DEVICE")
	if serial == "" {
		t.Skip("set PHONEVIDEO_TEST_DEVICE for real USB mode switching")
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
	available := false
	for _, d := range devices {
		if d.Serial == serial && slices.Contains(d.VideoModes, "h264") {
			available = true
		}
	}
	if !available {
		t.Skip("PhoneVideoTweak is not available on this phone")
	}
	for _, mode := range []string{"jpeg", "h264", "jpeg", "h264"} {
		m, err := a.StartMirror(serial, scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000, VideoMode: mode})
		if err != nil {
			t.Fatalf("start %s: %v", mode, err)
		}
		if m.Codec != mode || !slices.Contains(m.VideoModes, "h264") {
			t.Fatalf("wrong selected/available modes: %+v", m)
		}
		ws, _, err := websocket.DefaultDialer.Dial(m.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ws.SetReadDeadline(time.Now().Add(6 * time.Second))
		for {
			_, p, err := ws.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "jpeg" {
				if _, _, err := image.DecodeConfig(bytes.NewReader(p[12:])); err != nil {
					t.Fatal(err)
				}
				break
			}
			if binary.BigEndian.Uint64(p)&(uint64(1)<<63) == 0 {
				break
			}
		}
		a.StopMirror()
		_ = ws.Close()
		t.Logf("explicit %s: USB frame, selected codec, available modes, stop OK", mode)
	}
}
