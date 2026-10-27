package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"phonescreen/internal/usbmux/muxtest"
)

func TestIOSSettingsUSBAndPerPhonePreference(t *testing.T) {
	t.Setenv("ADB_PATH", filepath.Join(t.TempDir(), "missing-adb"))
	var mu sync.Mutex
	var savedPath string
	var savedBody map[string]any
	posts := 0
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/setAllSetting" {
			mu.Lock()
			posts++
			savedPath = r.URL.String()
			_ = json.NewDecoder(r.Body).Decode(&savedBody)
			mu.Unlock()
			if r.Method != http.MethodPost {
				t.Error("settings mutation must use POST")
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "data": "iPhone;;model;;name;;product"})
	}))
	defer phone.Close()
	mux := muxtest.Start(t, map[uint32]string{1: phone.Listener.Addr().String(), 2: phone.Listener.Addr().String()})
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.usbmux = mux.Client
	a.preferenceRoot = t.TempDir()
	info, err := a.GetIOSSettings("USB-ONE")
	if err != nil || info.Region != "US" || len(info.Choices) != 47 {
		t.Fatalf("%+v %v", info, err)
	}
	if err = a.RememberIOSSettingsRegion("USB-ONE", "jp"); err != nil {
		t.Fatal(err)
	}
	info, err = a.GetIOSSettings("USB-ONE")
	if err != nil || info.Region != "JP" {
		t.Fatalf("selection not remembered: %+v %v", info, err)
	}
	second, err := a.GetIOSSettings("USB-TWO")
	if err != nil || second.Region != "US" {
		t.Fatal("selection leaked across phones")
	}
	message, err := a.ApplyIOSSettings("USB-ONE", "US")
	if err != nil || !strings.Contains(message, "US") {
		t.Fatal(message, err)
	}
	mu.Lock()
	if posts != 1 || !strings.HasPrefix(savedPath, "/setAllSetting?") || savedBody["country"] != "United States" || len(savedBody["opts"].([]any)) != 13 {
		t.Error("wrong complete settings request")
	}
	mu.Unlock()
	a.mirrorSerial = "USB-ONE"
	if _, err = a.GetIOSSettings("USB-TWO"); err == nil {
		t.Fatal("mirror operated on another phone")
	}
	if _, err = a.ApplyIOSSettings("USB-TWO", "US"); err == nil {
		t.Fatal("mirror changed another phone")
	}
}
