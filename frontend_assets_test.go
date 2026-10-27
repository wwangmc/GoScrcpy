package main

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// Check the embedded files, not the source directory: a missing module prevents
// app.js from starting, so even device discovery and refresh stop working.
func TestEmbeddedFrontendImports(t *testing.T) {
	imports := regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"\n]+?\s+from\s+)?['"]([^'"]+)['"]`)
	err := fs.WalkDir(frontend, "frontend", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".js") {
			return nil
		}
		source, err := frontend.ReadFile(name)
		if err != nil {
			return err
		}
		for _, match := range imports.FindAllSubmatch(source, -1) {
			reference := string(match[1])
			if !strings.HasPrefix(reference, ".") {
				continue
			}
			module := path.Join(path.Dir(name), reference)
			if _, err := frontend.ReadFile(module); err != nil {
				t.Errorf("%s imports %s, but it is missing from the embedded frontend: %v", name, reference, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
