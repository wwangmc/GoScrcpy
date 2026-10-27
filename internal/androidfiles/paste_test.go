package androidfiles

import (
	"context"
	"os"
	"path"
	"testing"
	"time"
)

func TestAndroidPasteOwnedFiles(t *testing.T) {
	serial := os.Getenv("SCRCPY_TEST_DEVICE")
	if serial == "" {
		t.Skip("set SCRCPY_TEST_DEVICE for real copy/move test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	m := &Manager{}
	name := ".goscrcpy-paste-test-" + randomID()
	base := path.Join("/data/local/tmp", name)
	if err := m.Mkdir(ctx, serial, "storage", "/data/local/tmp", name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := m.Delete(context.Background(), serial, "storage", base); err != nil {
			t.Error(err)
		}
	}()
	source := path.Join(base, "a'$(echo nope); folder")
	dest := path.Join(base, "dest")
	moved := path.Join(base, "moved")
	keep := path.Join(base, "keep")
	alias := path.Join(base, "alias")
	nested := path.Join(source, "nested")
	script := "mkdir -- " + shellQuote(source) + " " + shellQuote(dest) + " " + shellQuote(moved) + " " + shellQuote(keep) + " && mkdir -- " + shellQuote(nested) + " && printf payload > " + shellQuote(path.Join(nested, "file")) + " && ln -s " + shellQuote(keep) + " " + shellQuote(path.Join(source, "link")) + " && ln -s /does-not-exist " + shellQuote(path.Join(source, "dangling")) + " && ln -s " + shellQuote(nested) + " " + shellQuote(alias)
	if _, err := m.shell(ctx, serial, script); err != nil {
		t.Fatal(err)
	}
	if err := m.Paste(ctx, serial, "storage", source, alias, false); err == nil {
		t.Fatal("copied into own subtree through a symlink")
	}
	if err := m.Paste(ctx, serial, "storage", source, dest, false); err != nil {
		t.Fatal(err)
	}
	copy := path.Join(dest, path.Base(source))
	check := "cmp " + shellQuote(path.Join(source, "nested/file")) + " " + shellQuote(path.Join(copy, "nested/file")) + " && [ -L " + shellQuote(path.Join(copy, "link")) + " ] && [ -L " + shellQuote(path.Join(copy, "dangling")) + " ] && [ -d " + shellQuote(keep) + " ]"
	if _, err := m.shell(ctx, serial, check); err != nil {
		t.Fatal("copy mismatch", err)
	}
	if err := m.Paste(ctx, serial, "storage", source, dest, false); err == nil {
		t.Fatal("copy replaced existing target")
	}
	if err := m.Paste(ctx, serial, "storage", source, dest, true); err == nil {
		t.Fatal("move replaced existing target")
	}
	if err := m.Paste(ctx, serial, "storage", source, moved, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.shell(ctx, serial, missingScript(source)+" && [ -f "+shellQuote(path.Join(moved, path.Base(source), "nested/file"))+" ]"); err != nil {
		t.Fatal("move did not relocate source", err)
	}
	listing, err := m.List(ctx, serial, "storage", dest)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != path.Base(source) {
		t.Fatalf("staging leak: %+v %v", listing, err)
	}
	t.Log("recursive copy, symlink preservation, conflicts, subtree rejection and move verified")
}
