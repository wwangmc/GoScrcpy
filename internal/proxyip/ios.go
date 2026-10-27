package proxyip

import (
	"context"
	"errors"
	"time"
)

type JSONCaller interface {
	JSON(context.Context, string, any) (string, error)
}
type Pause func(context.Context, time.Duration) error

func IOSCurlStoreURL(store string) (string, error) {
	return IOSPackageStoreURL(store, "curl")
}

func IOSPackageStoreURL(store, packageName string) (string, error) {
	if packageName != "curl" && packageName != "openssh" {
		return "", errors.New("不支持的商店软件包")
	}
	switch store {
	case "sileo":
		return "sileo://package/" + packageName, nil
	case "cydia":
		return "cydia://package/" + packageName, nil
	default:
		return "", errors.New("请选择 Sileo 或 Cydia 商店")
	}
}

func OpenIOSCurlStore(ctx context.Context, api JSONCaller, store string) error {
	return OpenIOSPackageStore(ctx, api, store, "curl")
}

func OpenIOSPackageStore(ctx context.Context, api JSONCaller, store, packageName string) error {
	url, err := IOSPackageStoreURL(store, packageName)
	if err != nil {
		return err
	}
	output, err := api.JSON(ctx, "/runShell", map[string]string{"cmd": ShellCommand("uiopen " + ShellQuote(url))})
	if err != nil {
		return err
	}
	if _, err = ShellOutput(output, false); err != nil {
		return errors.New("无法打开安装页，请确认手机已安装所选商店且 uiopen 可用")
	}
	return nil
}

func Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ChangeIOS stops on the first uncertain response; device mutations are never retried.
func ChangeIOS(ctx context.Context, api JSONCaller, config Import, pause Pause) error {
	if _, err := api.JSON(ctx, "/turnOffVPN", nil); err != nil {
		return err
	}
	if _, err := api.JSON(ctx, "/switchApp?pkgName=com.liguangming.Shadowrocket", nil); err != nil {
		return err
	}
	if err := pause(ctx, 600*time.Millisecond); err != nil {
		return err
	}
	for i, u := range []string{config.ShadowURL, "shadowrocket://route/proxy"} {
		output, err := api.JSON(ctx, "/runShell", map[string]string{"cmd": ShellCommand("uiopen " + ShellQuote(u))})
		if err != nil {
			return err
		}
		if _, err = ShellOutput(output, false); err != nil {
			return err
		}
		if err = pause(ctx, []time.Duration{1200 * time.Millisecond, 600 * time.Millisecond}[i]); err != nil {
			return err
		}
	}
	if _, err := api.JSON(ctx, "/turnOnVPN", nil); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		if err := pause(ctx, 800*time.Millisecond); err != nil {
			return err
		}
		status, err := api.JSON(ctx, "/vpnStatus", nil)
		if err != nil {
			return err
		}
		if status == "open" {
			return nil
		}
	}
	return errors.New("代理命令已执行，但手机 VPN 尚未开启，请检查 Shadowrocket 后查询 IP")
}
