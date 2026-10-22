package phone

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamExactAndShortInput(t *testing.T) {
	data := bytes.Repeat([]byte("abc"), 800000)
	var output bytes.Buffer
	var calls int
	n, err := stream(context.Background(), &output, bytes.NewReader(data), int64(len(data)), func(int64) { calls++ })
	if err != nil || n != int64(len(data)) || !bytes.Equal(output.Bytes(), data) || calls < 2 {
		t.Fatalf("chunked copy failed: %d %v", n, err)
	}
	output.Reset()
	_, err = stream(context.Background(), &output, bytes.NewReader([]byte("partial")), 100, func(int64) {})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("truncated source accepted: %v", err)
	}
}
func TestStreamCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stream(ctx, io.Discard, bytes.NewReader([]byte("data")), 4, func(int64) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
func TestChildRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", "..", ".", "../outside", "a/b", "a\\b", "\x00"} {
		if _, err := Child("/var/mobile", name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if got, err := Child("/var/mobile", "中文.txt"); err != nil || got != "/var/mobile/中文.txt" {
		t.Fatal(got, err)
	}
}
func TestPublishDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "temp")
	dst := filepath.Join(dir, "existing")
	_ = os.WriteFile(src, []byte("new"), 0600)
	_ = os.WriteFile(dst, []byte("old"), 0600)
	if err := publishFile(src, dst); err == nil {
		t.Fatal("overwrote existing destination")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "old" {
		t.Fatal("existing data changed")
	}
}

type duplex struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (d *duplex) Read(b []byte) (int, error)  { return d.in.Read(b) }
func (d *duplex) Write(b []byte) (int, error) { return d.out.Write(b) }
func TestRenameProtocolStatus(t *testing.T) {
	for _, status := range []uint64{0, 10} {
		response := make([]byte, 48)
		copy(response, []byte("CFA6LPAA"))
		binary.LittleEndian.PutUint64(response[8:], 48)
		binary.LittleEndian.PutUint64(response[16:], 48)
		binary.LittleEndian.PutUint64(response[24:], 0x7000000000000000)
		binary.LittleEndian.PutUint64(response[32:], 1)
		binary.LittleEndian.PutUint64(response[40:], status)
		rw := &duplex{in: bytes.NewReader(response)}
		err := renameAFC(rw, "/a", "/b")
		if (status == 0) != (err == nil) {
			t.Fatalf("status %d: %v", status, err)
		}
		sent := rw.out.Bytes()
		if binary.LittleEndian.Uint64(sent[32:]) != 0x18 || string(sent[40:]) != "/a\x00/b\x00" {
			t.Fatal("invalid rename request")
		}
	}
}
