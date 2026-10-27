package mks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"phonescreen/internal/usbmux/muxtest"
)

func TestBoundedShellQueryCanWaitForCommandHeaders(t *testing.T) {
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runShell" {
			t.Error("unexpected endpoint")
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(4100 * time.Millisecond):
		}
		_, _ = w.Write([]byte(`{"status":"success","data":"done"}`))
	}))
	defer phone.Close()
	mux := muxtest.Start(t, map[uint32]string{1: phone.Listener.Addr().String()})
	entries, err := mux.Client.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(mux.Client, entries[0])
	defer api.Close()
	value, err := api.JSONWithTimeout(context.Background(), "/runShell", map[string]string{"cmd": "test"}, 6*time.Second)
	if err != nil || value != "done" {
		t.Fatalf("bounded shell query prematurely timed out: value=%q error=%v", value, err)
	}
	if api.transport.ResponseHeaderTimeout != 4*time.Second {
		t.Fatal("query changed control timeout")
	}
}
