package scrcpy

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/adb"
)

// Explicit opt-in: this integration test starts mirroring and presses Home on the named device.
func TestDeviceMirror(t *testing.T) {
	serial := os.Getenv("SCRCPY_TEST_DEVICE")
	if serial == "" {
		t.Skip("set SCRCPY_TEST_DEVICE to run on a real Android device")
	}
	client, err := adb.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	server, err := filepath.Abs(filepath.Join("..", "..", "assets", "scrcpy-server-v"+Version))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(context.Background(), client, server, serial, Options{1920, 60, 8000000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.Video.SetReadDeadline(time.Now().Add(8 * time.Second))
	sawConfig, sawKey := false, false
	for i := 0; i < 20; i++ {
		packet, err := ReadPacket(s.Video)
		if err != nil {
			t.Fatalf("packet %d: %v; logs: %s", i, err, s.Logs())
		}
		sawConfig = sawConfig || packet[0]&0x80 != 0
		sawKey = sawKey || packet[0]&0x40 != 0
		if sawConfig && sawKey {
			break
		}
	}
	if !sawConfig || !sawKey {
		t.Fatalf("missing config/keyframe: %t/%t", sawConfig, sawKey)
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
	port := s.port
	s.Close()
	forward, err := client.Run(context.Background(), "forward", "--list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(forward, "tcp:"+port+" ") {
		t.Fatalf("forward leaked: %s", forward)
	}
	t.Logf("device=%s name=%s video=%dx%d H264 config+keyframe received; control sent; forward removed", serial, s.Metadata.Name, s.Metadata.Width, s.Metadata.Height)
}
