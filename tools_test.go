package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestCurrentPackage(t *testing.T) {
	for _, output := range []string{
		"mResumedActivity: ActivityRecord{aaa u0 com.android.settings/.Settings t9}",
		"topResumedActivity=ActivityRecord{bbb u0 com.example.app/com.example.Main t3}",
		"mCurrentFocus=Window{ccc u0 com.example.app/.Main}",
	} {
		if got, err := currentPackage(output); err != nil || got == "" {
			t.Fatalf("cannot read foreground application from %q: %q %v", output, got, err)
		}
	}
	for _, output := range []string{"", "mResumedActivity: null", "some text com.fake.app/.Main", "mCurrentFocus=Window{u0 invalid;command/.Main}"} {
		if got, err := currentPackage(output); err == nil {
			t.Fatalf("invalid foreground application accepted: %q", got)
		}
	}
}

func TestClipboardPackage(t *testing.T) {
	for _, text := range []string{
		"com.chaos.drink.sort",
		" \n com.chaos.drink.sort\t",
		"包名：com.chaos.drink.sort（安卓）",
		"market://details?id=com.chaos.drink.sort",
		"https://play.google.com/store/apps/details?id=com.chaos.drink.sort&hl=zh_CN",
		"包名 com.chaos.drink.sort\nhttps://play.google.com/store/apps/details?id=com.chaos.drink.sort",
	} {
		if got, err := clipboardPackage(text); err != nil || got != "com.chaos.drink.sort" {
			t.Errorf("clipboardPackage(%q) = %q, %v", text, got, err)
		}
	}
	for _, text := range []string{"", "没有包名", "android", "com..example", "1com.example.app", "com.123.app", "com.example.app com.other.app", "https://example.com", "https://play.google.com/store/apps/details?id=invalid"} {
		if got, err := clipboardPackage(text); err == nil {
			t.Errorf("invalid or ambiguous clipboard accepted: %q => %q", text, got)
		}
	}
}

func TestStoreUsesClipboardWithoutReadingForegroundApp(t *testing.T) {
	var calls [][]string
	run := func(args ...string) (string, error) {
		calls = append(calls, args)
		return "Starting: Intent", nil
	}
	if _, err := androidDeviceTool(run, "store", "包名：com.chaos.drink.sort"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "market://details?id=com.chaos.drink.sort"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected commands: %v", calls)
	}
	calls = nil
	if _, err := androidDeviceTool(run, "store", "invalid"); err == nil || len(calls) != 0 {
		t.Fatalf("invalid clipboard must not launch anything: %v %v", calls, err)
	}
	failure := errors.New("no app can handle market link")
	if _, err := androidDeviceTool(func(...string) (string, error) { return "", failure }, "store", "com.example.app"); !errors.Is(err, failure) {
		t.Fatalf("launch error lost: %v", err)
	}
}
