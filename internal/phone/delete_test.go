package phone

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"phonescreen/internal/afc"
)

func TestDeleteRejectsAmbiguousAndRootPaths(t *testing.T) {
	for _, p := range []string{"", "/", "//", ".", "..", "relative", "/a/..", "/a/../b", "/a/", "/a//b", "/a\x00b", "/a\\b"} {
		if _, err := DeletePath(p); err == nil {
			t.Fatalf("accepted deletion of %q", p)
		}
		m := &Manager{}
		if err := m.Delete(context.Background(), "unused", "media", p); err == nil {
			t.Fatalf("manager accepted %q", p)
		}
	}
	if got, err := DeletePath("/Documents/a'$(echo nope).txt"); err != nil || got != "/Documents/a'$(echo nope).txt" {
		t.Fatal(got, err)
	}
}

func TestIOSDeleteFileDirectoryAndSymlink(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		opcode uint64
		status uint64
	}{{"S_IFREG", 8, 0}, {"S_IFDIR", 0x22, 0}, {"S_IFLNK", 8, 0}, {"S_IFDIR", 0x22, 10}} {
		t.Run(fmt.Sprintf("%s-status-%d", tc.kind, tc.status), func(t *testing.T) {
			host, device := net.Pipe()
			defer host.Close()
			defer device.Close()
			_ = host.SetDeadline(time.Now().Add(3 * time.Second))
			_ = device.SetDeadline(time.Now().Add(3 * time.Second))
			m := &Manager{client: afc.NewFromConn(host), connection: host, serial: "iphone", scope: "media"}
			done := make(chan error, 1)
			const remote = "/Documents/a'$(echo nope)"
			go func() {
				for _, opcode := range []uint64{0xA, tc.opcode} {
					header := make([]byte, 40)
					if _, err := io.ReadFull(device, header); err != nil {
						done <- err
						return
					}
					payload := make([]byte, binary.LittleEndian.Uint64(header[8:])-40)
					if _, err := io.ReadFull(device, payload); err != nil {
						done <- err
						return
					}
					if binary.LittleEndian.Uint64(header[32:]) != opcode || string(payload) != remote {
						done <- fmt.Errorf("wrong deletion target/opcode: %q / %x", payload, header[32:])
						return
					}
					var body []byte
					if opcode == 0xA {
						binary.LittleEndian.PutUint64(header[32:], 2)
						body = []byte("st_ifmt\x00" + tc.kind + "\x00st_linktarget\x00/keep-this-target\x00")
						binary.LittleEndian.PutUint64(header[16:], 40)
					} else {
						binary.LittleEndian.PutUint64(header[32:], 1)
						body = make([]byte, 8)
						binary.LittleEndian.PutUint64(body, tc.status)
						binary.LittleEndian.PutUint64(header[16:], 48)
					}
					binary.LittleEndian.PutUint64(header[8:], uint64(40+len(body)))
					if _, err := device.Write(append(header, body...)); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			err := m.Delete(context.Background(), "iphone", "media", remote)
			if (err == nil) != (tc.status == 0) {
				t.Fatalf("delete status %d: %v", tc.status, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
