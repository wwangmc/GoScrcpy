package proxyip

import (
	"errors"
	"strings"
	"testing"
)

func TestAndroidCurlUsesSystemAndRejectsWrongArchitecture(t *testing.T) {
	pushes := 0
	push := func(string) error { pushes++; return nil }
	path, err := EnsureAndroidCurl(func(string) (string, error) { return "/system/bin/curl", nil }, push, "", "")
	if err != nil || path != "/system/bin/curl" || pushes != 0 {
		t.Fatal("system curl should not be replaced")
	}
	_, err = EnsureAndroidCurl(func(command string) (string, error) {
		if strings.Contains(command, "command -v") {
			return curlMissing, nil
		}
		return "armeabi-v7a", nil
	}, push, strings.Repeat("a", 64), "ab")
	if err == nil || pushes != 0 {
		t.Fatal("pushed ARM64 binary to an incompatible phone")
	}
}

func TestAndroidCurlInstallationIsVerifiedAtomicAndReused(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	pushed, published := false, false
	run := func(command string) (string, error) {
		switch {
		case strings.Contains(command, "command -v"):
			return curlMissing, nil
		case strings.HasPrefix(command, "getprop"):
			return "arm64-v8a,armeabi-v7a", nil
		case strings.HasPrefix(command, "toybox sha256sum"):
			if !pushed && !published {
				return "", errors.New("missing")
			}
			return checksum + "  curl", nil
		case strings.HasPrefix(command, "chmod 700"):
			if !pushed {
				t.Fatal("chmod before transfer")
			}
		case strings.HasSuffix(command, " --version"):
			if !pushed && !published {
				t.Fatal("ran incomplete binary")
			}
			return "curl 8.0.0 aarch64", nil
		case strings.HasPrefix(command, "mv -f"):
			if !pushed {
				t.Fatal("published before transfer")
			}
			published = true
		}
		return "", nil
	}
	path, err := EnsureAndroidCurl(run, func(remote string) error {
		if !strings.Contains(remote, ".upload-") {
			t.Fatal("overwrote installed file directly")
		}
		pushed = true
		return nil
	}, checksum, "ab")
	if err != nil || !published || path != "/data/local/tmp/goscrcpy/curl-"+checksum[:16] {
		t.Fatalf("path=%q error=%v", path, err)
	}
	_, err = EnsureAndroidCurl(run, func(string) error { t.Fatal("re-uploaded a verified working curl"); return nil }, checksum, "ab")
	if err != nil {
		t.Fatal(err)
	}
}

func TestAndroidCurlBadTransferNeverExecutesAndCleansPartialFile(t *testing.T) {
	cleaned, executed := false, false
	run := func(command string) (string, error) {
		switch {
		case strings.Contains(command, "command -v"):
			return curlMissing, nil
		case strings.HasPrefix(command, "getprop"):
			return "arm64-v8a", nil
		case strings.HasPrefix(command, "toybox sha256sum"):
			return "wrong-checksum", nil
		case strings.HasSuffix(command, " --version"):
			executed = true
		case strings.HasPrefix(command, "rm -f"):
			cleaned = true
		}
		return "", nil
	}
	_, err := EnsureAndroidCurl(run, func(string) error { return nil }, strings.Repeat("a", 64), "ab")
	if err == nil || executed || !cleaned {
		t.Fatal("used a corrupt or partial curl transfer")
	}
}
