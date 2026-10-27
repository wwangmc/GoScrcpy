package phone

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"phonescreen/internal/afc"
	"phonescreen/internal/usbmux"
)

func TestIOSFileServices(t *testing.T) {
	serial := os.Getenv("PHONEVIDEO_TEST_DEVICE")
	if serial == "" {
		t.Skip("set PHONEVIDEO_TEST_DEVICE (or auto) for USB file integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if serial == "auto" {
		devices, err := usbmux.NewClient().Devices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(devices) == 0 {
			t.Skip("no USB iPhone connected")
		}
		serial = devices[0].Properties.Serial
	}
	m := &Manager{}
	defer m.Close()
	for _, scope := range []string{"root", "media"} {
		t.Run(scope, func(t *testing.T) {
			listing, err := m.List(ctx, serial, scope, "/")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d entries", scope, len(listing.Entries))
		})
	}
	apps, err := SharedApps(ctx, serial)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d shared apps", len(apps))
	for _, app := range apps {
		t.Run("app:"+app.ID, func(t *testing.T) {
			listing, err := m.List(ctx, serial, "app:"+app.ID, "/Documents")
			if err != nil {
				t.Logf("device denied shared documents: %v", err)
				return
			}
			t.Logf("shared app: %d entries", len(listing.Entries))
		})
	}
	name := ".goscrcpy-test-" + randomName()
	folder := path.Join("/", name)
	if err = m.Mkdir(ctx, serial, "media", "/", name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		err := m.withClient(cleanup, serial, "media", func(c *afc.Client, _ io.ReadWriter) error {
			names, e := c.List(folder)
			if e != nil {
				return e
			}
			for _, name := range names {
				child, e := Child(folder, name)
				if e != nil {
					return e
				}
				if e = c.Remove(child); e != nil {
					return e
				}
			}
			return c.Remove(folder)
		})
		if err != nil {
			t.Error("cleanup:", err)
		}
	}()
	data := bytes.Repeat([]byte("USB\x00\xff测试"), 240000)
	local := filepath.Join(t.TempDir(), "测试文件.bin")
	if err = os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(ctx, serial, "media", folder, local, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(ctx, serial, "media", folder, local, func(int64, int64) {}); err == nil {
		t.Fatal("existing phone file overwritten")
	}
	remote := path.Join(folder, filepath.Base(local))
	renamed := "重命名.bin"
	if err = m.Rename(ctx, serial, "media", remote, renamed); err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(t.TempDir(), "download.bin")
	if err = m.Download(ctx, serial, "media", path.Join(folder, renamed), downloaded, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(downloaded)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("iOS roundtrip corrupted", err)
	}
	stopped, stop := context.WithCancel(ctx)
	if err = m.Upload(stopped, serial, "media", folder, local, func(done, total int64) {
		if done > 0 {
			stop()
		}
	}); err == nil {
		t.Fatal("cancel ignored")
	}
	listing, err := m.List(ctx, serial, "media", folder)
	if err != nil || len(listing.Entries) != 1 {
		t.Fatal(fmt.Sprintf("cancel leaked temp: %+v %v", listing, err))
	}
	t.Logf("iOS USB roundtrip verified: %d bytes; cancellation and no-overwrite", len(data))
}
