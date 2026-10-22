package usbmux_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/usbmux/muxtest"
)

func TestUSBDeviceIsolationAndPortByteOrder(t *testing.T) {
	var targets = map[uint32]string{}
	for id, name := range map[uint32]string{1: "first", 2: "second"} {
		name := name
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, name) }))
		t.Cleanup(server.Close)
		targets[id] = strings.TrimPrefix(server.URL, "http://")
	}
	mux := muxtest.Start(t, targets)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devices, err := mux.Client.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("Wi-Fi device was not filtered: %+v", devices)
	}
	for _, device := range devices {
		conn, err := mux.Client.Connect(ctx, device, 8081)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(conn, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
		out, err := io.ReadAll(conn)
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		expected := "first"
		if device.ID == 2 {
			expected = "second"
		}
		if !strings.HasSuffix(string(out), expected) {
			t.Fatalf("device crossed: %s", out)
		}
		if _, err = mux.Client.Connect(ctx, device, 8082); err == nil {
			t.Fatal("wrong remote port was accepted")
		}
		device.Properties.ConnectionType = "Network"
		if _, err = mux.Client.Connect(ctx, device, 8081); err == nil {
			t.Fatal("Wi-Fi connection was accepted")
		}
	}
}
