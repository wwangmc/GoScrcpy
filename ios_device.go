package main

import (
	"context"
	"errors"

	"phonescreen/internal/phonevideo"
)

func (a *App) iosAPI(ctx context.Context, serial string) (*phonevideo.API, error) {
	device, err := usbDevice(ctx, a.usbmux, serial)
	if err != nil {
		return nil, err
	}
	return phonevideo.NewAPI(a.usbmux, device), nil
}

func (a *App) beginIOSSettingsOperation() (func(), error) {
	if !a.iosSettingsMu.TryLock() {
		return nil, errors.New("正在执行手机设置，请等待完成")
	}
	return a.iosSettingsMu.Unlock, nil
}
