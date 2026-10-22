package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/phone"
)

func TestFileWindowSerialIsolation(t *testing.T) {
	for _, a := range []*App{{fileSerial: "ios-one"}, {mirrorSerial: "android-one"}} {
		if _, _, _, err := a.fileBackend("different-phone", ""); err == nil {
			t.Fatal("file window accepted a different device")
		}
	}
	a := &App{}
	if _, err := a.fileRequest("RenameFile", []string{"device"}); err == nil {
		t.Fatal("malformed file request accepted")
	}
}

type dropBackend struct {
	fileSystem
	entries []phone.Entry
	uploads []string
	upload  func(context.Context, string, func(int64, int64)) error
}

func (b *dropBackend) List(context.Context, string, string, string) (phone.Listing, error) {
	return phone.Listing{Entries: b.entries}, nil
}
func (b *dropBackend) Upload(ctx context.Context, serial, scope, parent, local string, p func(int64, int64)) error {
	if serial != "device" || scope != "media" || parent != "/Downloads" {
		return errors.New("wrong drop destination")
	}
	b.uploads = append(b.uploads, local)
	if b.upload != nil {
		return b.upload(ctx, local, p)
	}
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	p(info.Size(), info.Size())
	return nil
}
func dropSources(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "测试.exe"), filepath.Join(dir, "second.bin")}
	for i, p := range paths {
		if err := os.WriteFile(p, make([]byte, 10+i), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}
func TestDroppedBatchUploadsToChosenDirectory(t *testing.T) {
	a := &App{ctx: context.Background()}
	backend := &dropBackend{}
	paths := dropSources(t)
	n, err := a.uploadLocalFiles(backend, "device", "media", "/Downloads", paths)
	if err != nil || n != 2 || len(backend.uploads) != 2 {
		t.Fatalf("batch: %d %v %v", n, err, backend.uploads)
	}
	p := a.GetFileTransfer()
	if p.State != "done" || p.Done != 21 || p.Total != 21 || !strings.Contains(p.Name, "2/2") {
		t.Fatalf("wrong aggregate progress: %+v", p)
	}
}
func TestDropPreflightRejectsWholeInvalidBatch(t *testing.T) {
	for _, kind := range []string{"directory", "same-name", "existing"} {
		t.Run(kind, func(t *testing.T) {
			a := &App{ctx: context.Background()}
			paths := dropSources(t)
			b := &dropBackend{}
			switch kind {
			case "directory":
				paths[1] = t.TempDir()
			case "same-name":
				paths[1] = paths[0]
			case "existing":
				b.entries = []phone.Entry{{Name: filepath.Base(paths[1])}}
			}
			n, err := a.uploadLocalFiles(b, "device", "media", "/Downloads", paths)
			if err == nil || n != 0 || len(b.uploads) != 0 {
				t.Fatalf("partial invalid batch submitted: %d %v %v", n, err, b.uploads)
			}
		})
	}
}
func TestDroppedBatchCancelStopsRemainingFiles(t *testing.T) {
	a := &App{ctx: context.Background()}
	paths := dropSources(t)
	b := &dropBackend{}
	b.upload = func(ctx context.Context, local string, p func(int64, int64)) error {
		p(10, 10)
		a.CancelFileTransfer()
		return nil
	}
	n, err := a.uploadLocalFiles(b, "device", "media", "/Downloads", paths)
	if err == nil || n != 1 || len(b.uploads) != 1 || a.GetFileTransfer().State != "cancelled" {
		t.Fatalf("cancel did not stop batch: n=%d err=%v uploads=%v", n, err, b.uploads)
	}
}
func TestDroppedHostPathsAreNotAvailableToBrowserAPI(t *testing.T) {
	a := &App{}
	if _, err := a.UploadDroppedFiles("device", "media", "/", []string{`C:\private.txt`}); err == nil {
		t.Fatal("host paths accepted without native context")
	}
	if _, err := a.fileRequest("UploadDroppedFiles", []string{"device", "media", "/", `C:\private.txt`}); err == nil {
		t.Fatal("HTTP API accepted a host path")
	}
}
func TestTransferProgressAndCancellation(t *testing.T) {
	a := &App{ctx: context.Background()}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.transferFile("test.bin", "upload", func(ctx context.Context, progress func(int64, int64)) error {
			progress(64, 1024)
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("transfer did not start")
	}
	p := a.GetFileTransfer()
	if p.ID == "" || p.Name != "test.bin" || p.State != "running" || p.Done != 64 || p.Total != 1024 {
		t.Fatalf("bad progress: %+v", p)
	}
	a.CancelFileTransfer()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel was successful transfer")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not stop transfer")
	}
	if p = a.GetFileTransfer(); p.State != "cancelled" {
		t.Fatalf("wrong terminal state: %+v", p)
	}
	oldID := p.ID
	if err := a.transferFile("empty.bin", "download", func(_ context.Context, p func(int64, int64)) error { p(0, 0); return nil }); err != nil {
		t.Fatal(err)
	}
	if p = a.GetFileTransfer(); p.ID == oldID || p.State != "done" || p.Total != 0 {
		t.Fatalf("bad empty transfer: %+v", p)
	}
	if err := a.transferFile("denied.bin", "upload", func(context.Context, func(int64, int64)) error { return errors.New("permission denied") }); err == nil {
		t.Fatal("transfer error ignored")
	}
	if p = a.GetFileTransfer(); p.State != "error" || p.Error != "permission denied" {
		t.Fatalf("bad error state: %+v", p)
	}
}
func TestFileOperationsAreSerialized(t *testing.T) {
	a := &App{}
	if err := a.lockFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BrowseFiles("device", "", ""); err == nil {
		t.Fatal("browsing during transfer accepted")
	}
	a.files.ops.Unlock()
	if err := a.lockFiles(); err != nil {
		t.Fatal("file lock not reusable")
	}
	a.files.ops.Unlock()
}
