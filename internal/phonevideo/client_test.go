package phonevideo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"phonescreen/internal/usbmux/muxtest"
)

func TestControlUsesPhoneVideoPortWithLegacyServiceInstalled(t *testing.T) {
	var legacyCalls atomic.Int32
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls.Add(1)
		http.Error(w, "legacy plugin must not be used", http.StatusInternalServerError)
	}))
	defer legacy.Close()
	phone := &fakePhone{}
	service := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer service.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{
		1: {8081: legacy.Listener.Addr().String(), Port: service.Listener.Addr().String()},
	})
	devices, err := mux.Client.Devices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %+v %v", devices, err)
	}
	api := NewAPI(mux.Client, devices[0])
	defer api.Close()
	w, h, err := api.ScreenSize(context.Background())
	if err != nil || w != 828 || h != 1792 {
		t.Fatalf("PhoneVideo screen: %dx%d %v", w, h, err)
	}
	control, err := api.ConnectControl(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = control.Close()
	if _, err = api.JSON(context.Background(), "/runShell", map[string]string{"cmd": "printf probe"}); err != nil {
		t.Fatal(err)
	}
	if legacyCalls.Load() != 0 || mux.Connections.Load() != 2 {
		t.Fatalf("wrong control routing: legacy=%d channels=%d", legacyCalls.Load(), mux.Connections.Load())
	}
}

func TestLegacyServiceAloneRemainsAvailable(t *testing.T) {
	phone := &fakePhone{}
	legacy := httptest.NewServer(http.HandlerFunc(phone.handler))
	defer legacy.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {8081: legacy.Listener.Addr().String()}})
	devices, err := Scan(context.Background(), mux.Client)
	if err != nil || len(devices) != 1 || devices[0].State != "device" {
		t.Fatalf("legacy fallback: %+v %v", devices, err)
	}
}

func TestPhoneVideoErrorsDoNotReplayCommandsOnLegacy(t *testing.T) {
	var legacyCalls atomic.Int32
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { legacyCalls.Add(1) }))
	defer legacy.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "PhoneVideo service error", http.StatusInternalServerError)
	}))
	defer broken.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{
		1: {LegacyPort: legacy.Listener.Addr().String(), Port: broken.Listener.Addr().String()},
	})
	devices, err := mux.Client.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(mux.Client, devices[0])
	defer api.Close()
	if _, err = api.JSON(context.Background(), "/runShell", map[string]string{"cmd": "printf probe"}); err == nil {
		t.Fatal("expected PhoneVideo error")
	}
	if legacyCalls.Load() != 0 || api.ControlPort() != Port {
		t.Fatal("command was retried on legacy plugin")
	}
}
