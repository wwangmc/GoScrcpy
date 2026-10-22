package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"phonescreen/internal/phone"
)

type FileLocation struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

func (a *App) preferenceDirectory() (string, error) {
	if a.preferenceRoot != "" {
		return a.preferenceRoot, nil
	}
	if dir := os.Getenv("GOSCRCPY_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "GoScrcpy"), nil
}
func (a *App) readPreference(name string, value any) error {
	dir, err := a.preferenceDirectory()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func (a *App) writePreference(name string, value any) error {
	dir, err := a.preferenceDirectory()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".preferences-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(b); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filepath.Join(dir, name))
}
func fileLocationName(serial string) string {
	sum := sha256.Sum256([]byte(serial))
	return "files-" + hex.EncodeToString(sum[:]) + ".json"
}
func (a *App) checkFileSerial(serial string) error {
	if serial == "" || (a.mirrorSerial != "" && serial != a.mirrorSerial) || (a.fileSerial != "" && serial != a.fileSerial) {
		return errors.New("此窗口只能管理对应手机的文件")
	}
	return nil
}
func validFileLocation(location FileLocation) bool {
	return (location.Scope == "root" || location.Scope == "media" || location.Scope == "storage" || (strings.HasPrefix(location.Scope, "app:") && len(location.Scope) > 4)) && strings.HasPrefix(location.Path, "/") && !strings.ContainsRune(location.Path, 0)
}
func (a *App) GetFileLocation(serial string) (FileLocation, error) {
	if err := a.checkFileSerial(serial); err != nil {
		return FileLocation{}, err
	}
	var location FileLocation
	if err := a.readPreference(fileLocationName(serial), &location); err != nil {
		if os.IsNotExist(err) {
			return FileLocation{}, nil
		}
		return FileLocation{}, err
	}
	if !validFileLocation(location) {
		return FileLocation{}, nil
	}
	location.Path = phone.CleanPath(location.Path)
	return location, nil
}
func (a *App) rememberFileLocation(serial, scope, remote string) error {
	return a.writePreference(fileLocationName(serial), FileLocation{Scope: scope, Path: remote})
}
func (a *App) RememberDevice(serial string) error {
	if serial == "" || len(serial) > 256 {
		return errors.New("无效设备序列号")
	}
	return a.writePreference("selected-device.json", struct {
		Serial string `json:"serial"`
	}{serial})
}
func (a *App) lastDevice() string {
	var value struct {
		Serial string `json:"serial"`
	}
	if a.readPreference("selected-device.json", &value) != nil {
		return ""
	}
	return value.Serial
}
