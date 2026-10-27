package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phonescreen/internal/mobileshell"
	"phonescreen/internal/phonevideo"
	"phonescreen/internal/usbmux/muxtest"
)

func TestMissingOpenSSHPreventsTerminalAndOffersStores(t *testing.T) {
	testMissingOpenSSHPreventsTerminalAndOffersStores(t, phonevideo.Port)
}

func TestOpenSSHAuxiliaryLegacyFallback(t *testing.T) {
	testMissingOpenSSHPreventsTerminalAndOffersStores(t, phonevideo.LegacyPort)
}

func testMissingOpenSSHPreventsTerminalAndOffersStores(t *testing.T, port uint16) {
	t.Helper()
	t.Setenv("ADB_PATH", filepath.Join(t.TempDir(), "missing-adb"))
	var opened string
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := "iPhone;;device;;test;;product"
		if r.URL.Path == "/runShell" {
			var body struct {
				Cmd string `json:"cmd"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(body.Cmd, mobileshell.ProbeCommand) {
				data = "missing\n__GOSCRCPY_SHELL_EXIT__:0\n"
			} else {
				opened = body.Cmd
				data = "\n__GOSCRCPY_SHELL_EXIT__:0\n"
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "data": data})
	}))
	defer phone.Close()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {port: phone.Listener.Addr().String()}})
	a, err := NewApp(server)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.usbmux = mux.Client
	result, err := a.OpenShell("USB-ONE")
	if err != nil || !result.MissingOpenSSH {
		t.Fatalf("%+v %v", result, err)
	}
	if len(a.windows) != 0 {
		t.Fatal("opened a terminal when server is missing")
	}
	for _, store := range []string{"sileo", "cydia"} {
		if err = a.OpenOpenSSHStore("USB-ONE", store); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(opened, "uiopen '"+store+"://package/openssh'") {
			t.Fatal("wrong package or store", opened)
		}
	}
	if err = a.OpenOpenSSHStore("USB-ONE", "sileo;reboot"); err == nil {
		t.Fatal("accepted arbitrary store value")
	}
}

func TestSSHTrustMustBeConfirmedAndMatchesPhone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOSCRCPY_CONFIG_DIR", dir)
	out, err := os.CreateTemp(t.TempDir(), "console")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	c := &shellConsole{out: out}
	if err = c.trustSSHKey(bufio.NewReader(strings.NewReader("no\n")), "phone-one", "SHA256:first"); err == nil {
		t.Fatal("trusted without confirmation")
	}
	if err = c.trustSSHKey(bufio.NewReader(strings.NewReader("yes\n")), "phone-one", "SHA256:first"); err != nil {
		t.Fatal(err)
	}
	if err = c.trustSSHKey(bufio.NewReader(strings.NewReader("")), "phone-one", "SHA256:first"); err != nil {
		t.Fatal("did not reuse pinned phone identity", err)
	}
	if err = c.trustSSHKey(bufio.NewReader(strings.NewReader("yes\n")), "phone-one", "SHA256:changed"); err == nil {
		t.Fatal("accepted a changed host key")
	}
	if err = c.trustSSHKey(bufio.NewReader(strings.NewReader("no\n")), "phone-two", "SHA256:first"); err == nil {
		t.Fatal("reused trust for a different phone")
	}
}
