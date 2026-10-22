package androidfiles

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/adb"
)

func TestSyncUploadAndDownload(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 255, 128, 10}, 50000)
	desktop, device := net.Pipe()
	defer desktop.Close()
	defer device.Close()
	remote := "/sdcard/Download/a '测试.bin"
	done := make(chan error, 1)
	go func() {
		id, n, err := syncHeader(device)
		if err != nil {
			done <- err
			return
		}
		name := make([]byte, n)
		_, err = io.ReadFull(device, name)
		if err != nil || id != "SEND" || string(name) != remote+",33206" {
			done <- fmt.Errorf("bad SEND %s %q: %v", id, name, err)
			return
		}
		var received bytes.Buffer
		for {
			id, n, err = syncHeader(device)
			if err != nil {
				done <- err
				return
			}
			if id == "DONE" {
				break
			}
			if id != "DATA" || n > 65536 {
				done <- fmt.Errorf("bad DATA %s %d", id, n)
				return
			}
			if _, err = io.CopyN(&received, device, int64(n)); err != nil {
				done <- err
				return
			}
		}
		if !bytes.Equal(received.Bytes(), data) {
			done <- fmt.Errorf("upload corrupted")
			return
		}
		if err = syncWrite(device, "OKAY", nil); err != nil {
			done <- err
			return
		}
		id, n, err = syncHeader(device)
		if err != nil {
			done <- err
			return
		}
		name = make([]byte, n)
		_, err = io.ReadFull(device, name)
		if err != nil || id != "RECV" || string(name) != remote {
			done <- fmt.Errorf("bad RECV")
			return
		}
		for offset := 0; offset < len(data); offset += 65536 {
			if err = syncWrite(device, "DATA", data[offset:min(offset+65536, len(data))]); err != nil {
				done <- err
				return
			}
		}
		done <- syncWrite(device, "DONE", nil)
	}()
	c := &syncConn{Conn: desktop}
	var progress int64
	if err := send(context.Background(), c, remote, bytes.NewReader(data), int64(len(data)), func(n int64) { progress = n }); err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(data)) {
		t.Fatal("upload progress incomplete")
	}
	var output bytes.Buffer
	n, err := receive(context.Background(), c, remote, &output, func(n int64) { progress = n })
	if err != nil || n != int64(len(data)) || !bytes.Equal(output.Bytes(), data) || progress != n {
		t.Fatalf("download n=%d err=%v", n, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSyncRejectsInvalidAndCancelledData(t *testing.T) {
	for _, kind := range []string{"oversized", "truncated", "failure", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			host, remote := net.Pipe()
			defer host.Close()
			defer remote.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn := &syncConn{Conn: host}
			conn.stop = context.AfterFunc(ctx, func() { _ = host.Close() })
			go func() {
				_, n, err := syncHeader(remote)
				if err != nil {
					return
				}
				_, _ = io.CopyN(io.Discard, remote, int64(n))
				switch kind {
				case "oversized":
					var b [8]byte
					copy(b[:], "DATA")
					binary.LittleEndian.PutUint32(b[4:], 65537)
					_, _ = remote.Write(b[:])
				case "truncated":
					var b [8]byte
					copy(b[:], "DATA")
					binary.LittleEndian.PutUint32(b[4:], 10)
					_, _ = remote.Write(b[:])
					_, _ = remote.Write([]byte("short"))
					_ = remote.Close()
				case "failure":
					_ = syncWrite(remote, "FAIL", []byte("Permission denied"))
				case "cancelled":
					cancel()
				}
			}()
			if _, err := receive(ctx, conn, "/file", io.Discard, func(int64) {}); err == nil {
				t.Fatal("invalid/cancelled transfer accepted")
			}
		})
	}
}
func TestAndroidShellPathEscaping(t *testing.T) {
	if got := shellQuote("a'$(touch x); file"); got != "'a'\"'\"'$(touch x); file'" {
		t.Fatal(got)
	}
	if _, err := clean("/a\x00b"); err == nil {
		t.Fatal("NUL accepted")
	}
}

func TestAndroidFileRoundTrip(t *testing.T) {
	serial := os.Getenv("SCRCPY_TEST_DEVICE")
	if serial == "" {
		t.Skip("set SCRCPY_TEST_DEVICE for real file transfer test")
	}
	c, err := adb.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	m := Manager{Client: c}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := ".goscrcpy-test-" + randomID()
	folder := path.Join("/sdcard/Download", name)
	if err = m.Mkdir(ctx, serial, "storage", "/sdcard/Download", name); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.shell(context.Background(), serial, "rm -rf -- "+shellQuote(folder)) }()
	data := bytes.Repeat([]byte("binary\x00\xff测试"), 180000)
	localDir := t.TempDir()
	filename := "测试 a'$(echo no).bin"
	local := filepath.Join(localDir, filename)
	if err = os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	var uploaded int64
	if err = m.Upload(ctx, serial, "storage", folder, local, func(done, total int64) { uploaded = done }); err != nil {
		t.Fatal(err)
	}
	if uploaded != int64(len(data)) {
		t.Fatal("wrong upload progress")
	}
	listing, err := m.List(ctx, serial, "storage", folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != filename || listing.Entries[0].Size != int64(len(data)) || listing.Entries[0].Kind != "file" {
		t.Fatalf("wrong listing: %+v", listing)
	}
	if err = m.Upload(ctx, serial, "storage", folder, local, func(int64, int64) {}); err == nil {
		t.Fatal("overwrote existing phone file")
	}
	newName := "renamed 测试.bin"
	if err = m.Rename(ctx, serial, "storage", path.Join(folder, filename), newName); err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(localDir, "downloaded.bin")
	if err = m.Download(ctx, serial, "storage", path.Join(folder, newName), downloaded, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(downloaded)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("roundtrip corrupted", err)
	}
	if err = m.Download(ctx, serial, "storage", path.Join(folder, newName), downloaded, func(int64, int64) {}); err == nil {
		t.Fatal("overwrote local file")
	}
	cancelCtx, stop := context.WithCancel(ctx)
	if err = m.Upload(cancelCtx, serial, "storage", folder, local, func(done, total int64) {
		if done > 0 {
			stop()
		}
	}); err == nil {
		t.Fatal("cancel ignored")
	}
	after, err := m.List(ctx, serial, "storage", folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range after.Entries {
		if strings.HasPrefix(entry.Name, ".goscrcpy-") {
			t.Fatalf("staging file leaked: %+v", entry)
		}
	}
	t.Logf("USB upload/download verified: %d bytes, Unicode and shell characters preserved, no overwrite, cancellation cleaned", len(data))
}
