package proxyip

import (
	"errors"
	"fmt"
	"strings"
)

const curlMissing = "__GOSCRCPY_CURL_MISSING__"

// EnsureAndroidCurl reuses the phone's curl, or atomically installs the bundled
// ARM64 curl under /data/local/tmp. It never replaces Android system files.
// run and push are scoped to one USB device by the caller.
func EnsureAndroidCurl(run func(string) (string, error), push func(string) error, checksum, nonce string) (string, error) {
	output, err := run("if command -v curl >/dev/null 2>&1; then command -v curl; else printf " + ShellQuote(curlMissing) + "; fi")
	if err != nil {
		return "", errors.New("检查手机 curl 失败，请确认手机仍在线")
	}
	if strings.TrimSpace(output) != curlMissing {
		path := strings.TrimSpace(output)
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n\x00") {
			return "", errors.New("手机返回了无效 curl 路径")
		}
		return path, nil
	}
	abis, err := run("getprop ro.product.cpu.abilist")
	if err != nil {
		return "", errors.New("读取手机 CPU 架构失败")
	}
	arm64 := false
	for _, abi := range strings.Split(strings.TrimSpace(abis), ",") {
		if abi == "arm64-v8a" {
			arm64 = true
		}
	}
	if !arm64 {
		return "", errors.New("手机缺少 curl，项目内置 curl 仅支持 Android ARM64，此手机架构不匹配")
	}
	if len(checksum) != 64 || strings.Trim(checksum, "0123456789abcdef") != "" || nonce == "" || strings.Trim(nonce, "0123456789abcdef") != "" {
		return "", errors.New("无效内置 curl 标识")
	}
	dir := "/data/local/tmp/goscrcpy"
	target := dir + "/curl-" + checksum[:16]
	if hash, err := run("toybox sha256sum " + ShellQuote(target) + " 2>/dev/null"); err == nil && hashMatches(hash, checksum) {
		if version, err := run(ShellQuote(target) + " --version"); err == nil && strings.HasPrefix(strings.TrimSpace(version), "curl ") {
			return target, nil
		}
	}
	if _, err = run("mkdir -p " + ShellQuote(dir) + " && chmod 700 " + ShellQuote(dir)); err != nil {
		return "", errors.New("无法创建手机 curl 目录")
	}
	temp := target + ".upload-" + nonce
	published := false
	defer func() {
		if !published {
			_, _ = run("rm -f " + ShellQuote(temp))
		}
	}()
	if err = push(temp); err != nil {
		return "", fmt.Errorf("自动推送 curl 失败：%w", err)
	}
	hash, err := run("toybox sha256sum " + ShellQuote(temp))
	if err != nil || !hashMatches(hash, checksum) {
		return "", errors.New("手机 curl 文件校验失败，未启用该文件")
	}
	if _, err = run("chmod 700 " + ShellQuote(temp)); err != nil {
		return "", errors.New("设置手机 curl 执行权限失败")
	}
	version, err := run(ShellQuote(temp) + " --version")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(version), "curl ") {
		return "", errors.New("内置 curl 无法在此手机运行，请检查安卓版本及运行库兼容性")
	}
	if _, err = run("mv -f " + ShellQuote(temp) + " " + ShellQuote(target)); err != nil {
		return "", errors.New("启用手机 curl 失败")
	}
	published = true
	return target, nil
}
func hashMatches(output, checksum string) bool {
	fields := strings.Fields(output)
	return len(fields) >= 1 && fields[0] == checksum
}
