package mobileshell

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type JSONCaller interface {
	JSON(context.Context, string, any) (string, error)
}

const exitMarker = "__GOSCRCPY_SHELL_EXIT__:"

func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func ShellCommand(command string) string {
	return command + ` 2>&1; gs_shell_exit=$?; printf '\n` + exitMarker + `%s\n' "$gs_shell_exit"`
}

func ShellOutput(output string) (string, error) {
	i := strings.LastIndex(output, "\n"+exitMarker)
	if i < 0 {
		return "", errors.New("手机命令未返回完成标记，无法确认执行结果")
	}
	code, err := strconv.Atoi(strings.TrimSpace(output[i+len(exitMarker)+1:]))
	if err != nil {
		return "", errors.New("手机命令返回无效退出码")
	}
	if code != 0 {
		return "", fmt.Errorf("手机命令执行失败（退出码 %d），请检查手机命令及所需工具", code)
	}
	return strings.TrimSpace(output[:i]), nil
}

func OpenSSHStoreURL(store string) (string, error) {
	switch store {
	case "sileo", "cydia":
		return store + "://package/openssh", nil
	default:
		return "", errors.New("请选择 Sileo 或 Cydia 商店")
	}
}

func OpenSSHStore(ctx context.Context, api JSONCaller, store string) error {
	url, err := OpenSSHStoreURL(store)
	if err != nil {
		return err
	}
	output, err := api.JSON(ctx, "/runShell", map[string]string{"cmd": ShellCommand("uiopen " + ShellQuote(url))})
	if err != nil {
		return err
	}
	if _, err = ShellOutput(output); err != nil {
		return errors.New("无法打开安装页，请确认手机已安装所选商店且 uiopen 可用")
	}
	return nil
}
