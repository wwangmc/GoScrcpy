package mks

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux/muxtest"
)

func TestVideoModesAreDetectedPerPhoneWithoutStartingEncoder(t *testing.T) {
	phone := &fakePhone{}
	api := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer api.Close()
	video, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	var startBytes atomic.Int32
	go func() {
		for {
			c, err := video.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); b, _ := io.ReadAll(c); startBytes.Add(int32(len(b))) }(c)
		}
	}()
	address := strings.TrimPrefix(api.URL, "http://")
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{
		1: {Port: address, PhoneVideoPort: video.Addr().String()}, 2: {Port: address},
	})
	devices, err := Scan(context.Background(), mux.Client)
	if err != nil || len(devices) != 2 {
		t.Fatalf("scan: %+v %v", devices, err)
	}
	for _, d := range devices {
		want := []string{"jpeg"}
		if d.Serial == "USB-ONE" {
			want = append(want, "h264")
		}
		if !slices.Equal(d.VideoModes, want) {
			t.Fatalf("wrong capabilities: %+v", d)
		}
	}
	if startBytes.Load() != 0 {
		t.Fatal("capability detection started an encoder")
	}
}

func TestExplicitScreenshotDoesNotOpenInstalledVideoPlugin(t *testing.T) {
	phone := &fakePhone{}
	api := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer api.Close()
	video, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {Port: strings.TrimPrefix(api.URL, "http://"), PhoneVideoPort: video.Addr().String()}})
	s, err := Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000, VideoMode: "jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Encoding() != "jpeg" {
		t.Fatal("ignored requested screenshot mode")
	}
	if _, err = s.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	// Exactly screenshot HTTP + input WS; an unwanted video dial adds a third.
	if mux.Connections.Load() != 2 {
		t.Fatalf("unexpected USB channel count: %d", mux.Connections.Load())
	}
	if slices.Contains(s.Settings(), "bitRate") {
		t.Fatal("screenshot advertised video bitrate")
	}
}

func TestExplicitH264WithoutPluginCannotFallBack(t *testing.T) {
	phone := &fakePhone{}
	api := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer api.Close()
	mux := muxtest.Start(t, map[uint32]string{1: strings.TrimPrefix(api.URL, "http://")})
	_, err := Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000, VideoMode: "h264"})
	if err == nil || !strings.Contains(err.Error(), "PhoneVideoTweak") {
		t.Fatalf("expected missing plugin error, got %v", err)
	}
	if phone.polls.Load() != 0 || phone.originals.Load() != 0 {
		t.Fatal("explicit H264 fell back to screenshots")
	}
}
