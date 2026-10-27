package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"phonescreen/internal/iossettings"
)

type IOSSettingsInfo struct {
	Choices []iossettings.Choice `json:"choices"`
	Region  string               `json:"region"`
}

func (a *App) iosSettingsDevice(serial string) error {
	if serial == "" || a.fileSerial != "" || (a.mirrorSerial != "" && a.mirrorSerial != serial) {
		return errors.New("此窗口只能设置对应的 iOS 手机")
	}
	devices, err := a.GetDevices()
	if err != nil {
		return err
	}
	for _, device := range devices {
		if device.Serial == serial {
			if device.Platform != "ios" {
				return errors.New("一键设置仅支持 iOS 手机")
			}
			if device.State != "device" {
				return errors.New("手机设置服务未就绪，请确认 PhoneVideoTweak 或 mks_tweak 正常运行")
			}
			return nil
		}
	}
	return errors.New("未找到对应的 iOS 手机，请检查 USB 连接")
}
func iosSettingsPreferenceName(serial string) string {
	sum := sha256.Sum256([]byte(serial))
	return "ios-settings-" + hex.EncodeToString(sum[:]) + ".json"
}
func (a *App) GetIOSSettings(serial string) (IOSSettingsInfo, error) {
	if err := a.iosSettingsDevice(serial); err != nil {
		return IOSSettingsInfo{}, err
	}
	choices, err := iossettings.Choices()
	if err != nil {
		return IOSSettingsInfo{}, err
	}
	info := IOSSettingsInfo{Choices: choices, Region: "US"}
	var saved struct {
		Region string `json:"region"`
	}
	if err = a.readPreference(iosSettingsPreferenceName(serial), &saved); err != nil && !os.IsNotExist(err) {
		return info, err
	}
	if choice, err := iossettings.Lookup(saved.Region); err == nil {
		info.Region = choice.Region
	}
	return info, nil
}
func (a *App) RememberIOSSettingsRegion(serial, region string) error {
	if err := a.iosSettingsDevice(serial); err != nil {
		return err
	}
	choice, err := iossettings.Lookup(region)
	if err != nil {
		return err
	}
	return a.writePreference(iosSettingsPreferenceName(serial), struct {
		Region string `json:"region"`
	}{choice.Region})
}
func (a *App) ApplyIOSSettings(serial, region string) (string, error) {
	done, err := a.beginIOSSettingsOperation()
	if err != nil {
		return "", err
	}
	defer done()
	if err = a.iosSettingsDevice(serial); err != nil {
		return "", err
	}
	choice, err := iossettings.Lookup(region)
	if err != nil {
		return "", err
	}
	if err = a.writePreference(iosSettingsPreferenceName(serial), struct {
		Region string `json:"region"`
	}{choice.Region}); err != nil {
		return "", fmt.Errorf("无法记住地区选择：%w", err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	api, err := a.iosAPI(ctx, serial)
	if err != nil {
		return "", err
	}
	defer api.Close()
	if _, err = iossettings.Apply(ctx, api, choice.Region); err != nil {
		return "", fmt.Errorf("未确认一键设置完成：%w。部分设置可能已提交，请先查看手机状态再操作", err)
	}
	return "手机已接收「" + choice.Name + " / " + choice.Region + "」的语言、地区和时区设置，系统设置将异步生效。", nil
}
