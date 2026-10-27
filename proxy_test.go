package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"phonescreen/internal/proxyip"
)

func TestProxyPreferencesDeviceIsolationAndBusyGuard(t *testing.T) {
	a := &App{preferenceRoot: t.TempDir(), mirrorSerial: "phone-one"}
	o, err := a.GetProxyOptions("phone-one")
	if err != nil || o.AppID != "com.mobile.legends" || o.Name != "cs" {
		t.Fatalf("%+v %v", o, err)
	}
	o.ProxyID = "chosen"
	o.Region = "jp"
	if err = a.RememberProxyOptions("phone-one", o); err != nil {
		t.Fatal(err)
	}
	b := &App{preferenceRoot: a.preferenceRoot, mirrorSerial: "phone-one"}
	loaded, err := b.GetProxyOptions("phone-one")
	if err != nil || loaded.Region != "JP" || loaded.ProxyID != "chosen" {
		t.Fatalf("%+v %v", loaded, err)
	}
	if err = b.RememberProxyOptions("phone-two", proxyip.Defaults()); err == nil {
		t.Fatal("operated another phone")
	}
	raw, err := os.ReadFile(filepath.Join(a.preferenceRoot, proxyPreferenceName("phone-one")))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 4 {
		t.Fatal("unexpected persisted proxy fields")
	}
	done, err := a.beginProxyOperation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.beginProxyOperation(); err == nil {
		t.Fatal("allowed overlapping proxy operations")
	}
	done()
	done, err = a.beginProxyOperation()
	if err != nil {
		t.Fatal("operation lock was not released")
	}
	done()
}
