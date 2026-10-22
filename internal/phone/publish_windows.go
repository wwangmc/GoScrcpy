package phone

import "golang.org/x/sys/windows"

// MoveFile (without REPLACE_EXISTING) publishes a completed file without overwriting.
func publishFile(source, target string) error {
	s, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFile(s, t)
}
