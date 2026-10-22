package main

import (
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWindowProcessHelper(t *testing.T) {
	if os.Getenv("GOSCRCPY_WINDOW_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestIndependentWindowLifecycle(t *testing.T) {
	a := &App{}
	newChild := func(serial string) io.WriteCloser {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowProcessHelper$")
		cmd.Env = append(os.Environ(), "GOSCRCPY_WINDOW_HELPER=1")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := a.trackWindow(serial, cmd); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = input.Close() })
		return input
	}
	waitCount := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for len(a.GetOpenDevices()) != want {
			if time.Now().After(deadline) {
				t.Fatalf("open windows: %v, want %d", a.GetOpenDevices(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	first, second := newChild("android"), newChild("ios")
	fileWindow := newChild("files:android")
	waitCount(2)
	_ = fileWindow.Close()
	if err := a.trackWindow("android", exec.Command("must-not-be-launched")); err == nil {
		t.Fatal("duplicate window accepted")
	}
	_ = first.Close()
	waitCount(1)
	if got := a.GetOpenDevices(); len(got) != 1 || got[0] != "ios" {
		t.Fatalf("closing Android affected iOS: %v", got)
	}
	reopened := newChild("android")
	waitCount(2)
	_ = second.Close()
	_ = reopened.Close()
	waitCount(0)
	if err := a.trackWindow("failed", exec.Command("missing-goscrcpy-executable")); err == nil {
		t.Fatal("failed launch succeeded")
	}
	waitCount(0)
}
