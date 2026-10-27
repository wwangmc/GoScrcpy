package proxyip

import (
	"context"
	"testing"
)

type storeAPI struct {
	calls   int
	command string
	output  string
}

func (a *storeAPI) JSON(_ context.Context, path string, body any) (string, error) {
	a.calls++
	a.command = body.(map[string]string)["cmd"]
	if path != "/runShell" {
		panic("unexpected endpoint")
	}
	return a.output, nil
}
func TestIOSCurlStoreUsesExactAllowedURLs(t *testing.T) {
	for _, store := range []string{"sileo", "cydia"} {
		a := &storeAPI{output: "\n" + exitMarker + "0\n"}
		if err := OpenIOSCurlStore(context.Background(), a, store); err != nil {
			t.Fatal(err)
		}
		if a.calls != 1 || a.command != ShellCommand("uiopen '"+store+"://package/curl'") {
			t.Fatalf("wrong store command: %q", a.command)
		}
	}
	a := &storeAPI{}
	for _, invalid := range []string{"", "https://example.com", "sileo://package/other", "sileo;reboot"} {
		if err := OpenIOSCurlStore(context.Background(), a, invalid); err == nil || a.calls != 0 {
			t.Fatal("accepted arbitrary store command")
		}
	}
	a.output = "\n" + exitMarker + "1\n"
	if err := OpenIOSCurlStore(context.Background(), a, "sileo"); err == nil {
		t.Fatal("claimed store opened after command failure")
	}
}
