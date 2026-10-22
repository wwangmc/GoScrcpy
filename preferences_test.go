package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesSurviveNewWindowAndRestart(t *testing.T) {
	dir := t.TempDir()
	first := &App{preferenceRoot: dir}
	if err := first.RememberDevice("iphone-one"); err != nil {
		t.Fatal(err)
	}
	if err := first.rememberFileLocation("iphone-one", "media", "/Downloads"); err != nil {
		t.Fatal(err)
	}
	if err := first.rememberFileLocation("android-two", "storage", "/sdcard/Pictures"); err != nil {
		t.Fatal(err)
	}
	second := &App{preferenceRoot: dir, fileSerial: "iphone-one"}
	if second.GetLaunchOptions().LastDevice != "iphone-one" {
		t.Fatal("selected device lost on restart")
	}
	saved, err := second.GetFileLocation("iphone-one")
	if err != nil || saved.Scope != "media" || saved.Path != "/Downloads" {
		t.Fatalf("location lost: %+v %v", saved, err)
	}
	if err = second.rememberFileLocation("iphone-one", "root", "/var/mobile"); err != nil {
		t.Fatal(err)
	}
	third := &App{preferenceRoot: dir}
	saved, err = third.GetFileLocation("iphone-one")
	if err != nil || saved.Scope != "root" || saved.Path != "/var/mobile" {
		t.Fatalf("last choice not replaced: %+v %v", saved, err)
	}
	saved, err = third.GetFileLocation("android-two")
	if err != nil || saved.Scope != "storage" || saved.Path != "/sdcard/Pictures" {
		t.Fatal("another device's preferences changed", saved, err)
	}
}
func TestFilePreferencesAreBoundToDeviceAndHandleBadData(t *testing.T) {
	dir := t.TempDir()
	a := &App{preferenceRoot: dir, fileSerial: "one"}
	if _, err := a.GetFileLocation("two"); err == nil {
		t.Fatal("read another device's location")
	}
	if saved, err := a.GetFileLocation("one"); err != nil || saved.Scope != "" {
		t.Fatal("missing preference not handled", saved, err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileLocationName("one")), []byte(`{"scope":"arbitrary","path":"/"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if saved, err := a.GetFileLocation("one"); err != nil || saved.Scope != "" {
		t.Fatal("invalid preference restored", saved, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "selected-device.json"), []byte("bad JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.lastDevice() != "" {
		t.Fatal("bad device preference restored")
	}
}
