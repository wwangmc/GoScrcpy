package main

import (
	"context"
	"errors"

	"phonescreen/internal/mks"
)

func (a *App) iosAPI(ctx context.Context, serial string) (*mks.API, error) {
	device, err := usbDevice(ctx, a.usbmux, serial)
	if err != nil {
		return nil, err
	}
	return mks.NewAPI(a.usbmux, device), nil
}

func (a *App) beginIOSSettingsOperation() (func(), error) {
	if !a.iosSettingsMu.TryLock() {
		return nil, errors.New("正在执行手机设置，请等待完成")
	}
	return a.iosSettingsMu.Unlock, nil
}
