//go:build !windows

package phone

import "os"

func publishFile(source, target string) error { return os.Link(source, target) }
