package mks

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux/muxtest"
)

func TestPhoneVideoUSBAndExistingInput(t *testing.T) {
	phone := &fakePhone{}
	api := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer api.Close()
	video, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	requests := make(chan [16]byte, 2)
	config := videoPacket(1<<63, []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0x28, 0, 0, 0, 1, 0x68, 0xce})
	frame := videoPacket(1<<62|1234567, []byte{0, 0, 0, 1, 0x65, 0xaa})
	go func() {
		for {
			c, err := video.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var request [16]byte
				if _, err := io.ReadFull(c, request[:]); err != nil {
					return
				}
				requests <- request
				var meta [76]byte
				copy(meta[:], "PhoneVideo Test")
				copy(meta[64:], "h264")
				binary.BigEndian.PutUint32(meta[68:], 640)
				binary.BigEndian.PutUint32(meta[72:], 1384)
				_, _ = c.Write(meta[:])
				_, _ = c.Write(config)
				_, _ = c.Write(frame)
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	targets := map[uint32]map[uint16]string{}
	for _, id := range []uint32{1, 2} {
		targets[id] = map[uint16]string{Port: strings.TrimPrefix(api.URL, "http://"), PhoneVideoPort: video.Addr().String()}
	}
	mux := muxtest.StartPorts(t, targets)
	options := scrcpy.Options{MaxSize: 1920, MaxFPS: 120, BitRate: 8000000}
	sessions := make([]*Session, 0, 2)
	for _, serial := range []string{"USB-ONE", "USB-TWO"} {
		s, err := Start(context.Background(), mux.Client, serial, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		sessions = append(sessions, s)
		if s.Encoding() != "h264" || s.Info().Width != 640 || !slices.Contains(s.Settings(), "bitRate") {
			t.Fatalf("unexpected video session: %+v", s)
		}
		request := <-requests
		if string(request[:4]) != "PMV1" || binary.BigEndian.Uint32(request[4:]) != 60 || binary.BigEndian.Uint32(request[8:]) != 8000000 || binary.BigEndian.Uint32(request[12:]) != 1920 {
			t.Fatalf("wrong handshake: %x", request)
		}
		for _, want := range [][]byte{config, frame} {
			got, err := s.ReadFrame()
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("packet changed: %x %v", got, err)
			}
		}
		packet := make([]byte, 32)
		packet[0] = 2
		binary.BigEndian.PutUint32(packet[10:], 320)
		binary.BigEndian.PutUint32(packet[14:], 692)
		binary.BigEndian.PutUint16(packet[18:], 640)
		binary.BigEndian.PutUint16(packet[20:], 1384)
		if err := s.WriteControl(packet); err != nil {
			t.Fatal(err)
		}
		packet[1] = 1
		if err := s.WriteControl(packet); err != nil {
			t.Fatal(err)
		}
	}
	if phone.polls.Load() != 0 || phone.originals.Load() != 0 {
		t.Fatal("H.264 session requested a screenshot")
	}
	deadline := time.Now().Add(time.Second)
	for {
		phone.mu.Lock()
		calls := slices.Clone(phone.calls)
		phone.mu.Unlock()
		if len(calls) >= 4 {
			for _, call := range calls {
				if call.Path == "ws:touch" && (call.Body["x"] != float64(414) || call.Body["y"] != float64(896)) {
					t.Fatalf("scaled coordinates wrong: %+v", call)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("input not delivered independently of video")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Closing video must unblock readers, including an idle screen, on both phones.
	for _, s := range sessions {
		done := make(chan error, 1)
		go func(s *Session) { _, err := s.ReadFrame(); done <- err }(s)
		s.Close()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("read unexpectedly succeeded after close")
			}
		case <-time.After(time.Second):
			t.Fatal("video read leaked")
		}
	}
}

func videoPacket(flags uint64, data []byte) []byte {
	b := make([]byte, 12+len(data))
	binary.BigEndian.PutUint64(b, flags)
	binary.BigEndian.PutUint32(b[8:], uint32(len(data)))
	copy(b[12:], data)
	return b
}

func TestPhoneVideoBrokenPluginDoesNotSilentlyUseScreenshots(t *testing.T) {
	phone := &fakePhone{}
	api := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer api.Close()
	video, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	go func() {
		c, err := video.Accept()
		if err == nil {
			c.Close()
		}
	}()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {Port: strings.TrimPrefix(api.URL, "http://"), PhoneVideoPort: video.Addr().String()}})
	_, err = Start(context.Background(), mux.Client, "USB-ONE", scrcpy.Options{MaxSize: 1280, MaxFPS: 30, BitRate: 4000000})
	if err == nil || !strings.Contains(err.Error(), "PhoneVideo") {
		t.Fatalf("missing plugin error: %v", err)
	}
	if phone.polls.Load() != 0 {
		t.Fatal("broken plugin silently fell back")
	}
}
