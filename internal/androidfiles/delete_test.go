package androidfiles

import (
	"context"
	"os"
	"path"
	"testing"
	"time"
)

func TestDeleteRejectsRootBeforeADB(t *testing.T) {
	m := &Manager{}
	for _, remote := range []string{"", "/", "/sdcard/..", "/sdcard/test/", "relative", "/a\x00b"} {
		if err := m.Delete(context.Background(), "unused", "storage", remote); err == nil {
			t.Fatalf("accepted %q", remote)
		}
	}
}

func TestAndroidDeleteOwnedFiles(t *testing.T) {
	serial := os.Getenv("SCRCPY_TEST_DEVICE")
	if serial == "" {
		t.Skip("set SCRCPY_TEST_DEVICE for real deletion test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	m := &Manager{}
	name := ".goscrcpy-delete-test-" + randomID()
	folder := path.Join("/data/local/tmp", name)
	if err := m.Mkdir(ctx, serial, "storage", "/data/local/tmp", name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := m.Delete(context.Background(), serial, "storage", folder); err != nil {
			t.Error(err)
		}
	}()
	keep := path.Join(folder, "keep")
	link := path.Join(folder, "link")
	nested := path.Join(folder, "a'$(echo nope); folder")
	file := path.Join(folder, "a'$(echo nope); file")
	script := "mkdir -- " + shellQuote(keep) + " " + shellQuote(nested) + " && printf keep > " + shellQuote(path.Join(keep, "sentinel")) + " && ln -s " + shellQuote(keep) + " " + shellQuote(link) + " && printf data > " + shellQuote(path.Join(nested, "child")) + " && printf data > " + shellQuote(file)
	if _, err := m.shell(ctx, serial, script); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{link, nested, file} {
		if err := m.Delete(ctx, serial, "storage", target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.shell(ctx, serial, "[ -f "+shellQuote(path.Join(keep, "sentinel"))+" ]"); err != nil {
		t.Fatal("symlink target was removed", err)
	}
	listing, err := m.List(ctx, serial, "storage", folder)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "keep" {
		t.Fatalf("wrong entries: %+v, %v", listing, err)
	}
	if err := m.Delete(ctx, serial, "storage", file); err == nil {
		t.Fatal("missing file incorrectly reported as deleted")
	}
	t.Log("files, non-empty folders, quoted filenames and symlink target preservation verified")
}
