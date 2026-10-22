package androidfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"phonescreen/internal/adb"
	"phonescreen/internal/phone"
)

type Manager struct{ Client adb.Client }

func (m *Manager) client() (adb.Client, error) {
	if m.Client.Path != "" {
		return m.Client, nil
	}
	return adb.Resolve()
}

// ADB shell joins arguments before passing them to the device's shell. Quote
// every path, including names supplied by the device, as one POSIX shell word.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func clean(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", errors.New("无效文件路径")
	}
	return phone.CleanPath(p), nil
}
func (m *Manager) shell(ctx context.Context, serial, script string) (string, error) {
	c, err := m.client()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := c.Command(ctx, "-s", serial, "shell", "-T", script).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return "", errors.New("无法完成手机文件操作：目录不存在、没有访问权限或设备已断开")
		}
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return "", fmt.Errorf("手机文件操作失败：%s", detail)
	}
	return strings.TrimSpace(string(output)), nil
}
func (m *Manager) List(ctx context.Context, serial, scope, p string) (phone.Listing, error) {
	started := time.Now()
	p, err := clean(p)
	result := phone.Listing{Path: p, Entries: []phone.Entry{}}
	if err != nil {
		return result, err
	}
	// NUL-delimited fields preserve spaces, Unicode, tabs and newlines in names.
	// One shell invocation avoids a USB round trip per entry. Symlinks remain
	// visible and can be navigated; only regular files can be downloaded.
	script := "p=" + shellQuote(p) + "; [ -d \"$p\" ] && [ -r \"$p\" ] && [ -x \"$p\" ] || exit 1; for f in \"$p\"/.[!.]* \"$p\"/..?* \"$p\"/*; do [ -e \"$f\" ] || [ -L \"$f\" ] || continue; stat -c '%f %s' -- \"$f\" || exit 1; printf '\\0%s\\0' \"$f\"; done"
	output, err := m.shell(ctx, serial, script)
	if err != nil {
		return result, err
	}
	fields := strings.Split(output, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		metadata := strings.Fields(fields[i])
		if len(metadata) != 2 {
			return result, errors.New("无效 Android 文件属性")
		}
		mode, e := strconv.ParseUint(metadata[0], 16, 32)
		if e != nil {
			return result, fmt.Errorf("无法解析 Android 文件属性：%w", e)
		}
		size, e := strconv.ParseInt(metadata[1], 10, 64)
		if e != nil {
			return result, e
		}
		full := fields[i+1]
		name := path.Base(full)
		if _, e = phone.Child(p, name); e != nil {
			continue
		}
		kind := "other"
		switch mode & 0170000 {
		case 0040000:
			kind = "directory"
		case 0100000:
			kind = "file"
		case 0120000:
			kind = "link"
		}
		result.Entries = append(result.Entries, phone.Entry{Name: name, Path: path.Join(p, name), Kind: kind, Size: size})
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}
func missingScript(target string) string {
	return "[ ! -e " + shellQuote(target) + " ] && [ ! -L " + shellQuote(target) + " ]"
}
func (m *Manager) Mkdir(ctx context.Context, serial, scope, parent, name string) error {
	target, err := phone.Child(parent, name)
	if err != nil {
		return err
	}
	_, err = m.shell(ctx, serial, missingScript(target)+" && mkdir -- "+shellQuote(target))
	return err
}
func (m *Manager) Rename(ctx context.Context, serial, scope, source, name string) error {
	source, err := clean(source)
	if err != nil {
		return err
	}
	if source == "/" {
		return errors.New("不能重命名根目录")
	}
	target, err := phone.Child(path.Dir(source), name)
	if err != nil {
		return err
	}
	if source == target {
		return nil
	}
	_, err = m.shell(ctx, serial, missingScript(target)+" && mv -n -- "+shellQuote(source)+" "+shellQuote(target)+" && [ ! -e "+shellQuote(source)+" ] && [ ! -L "+shellQuote(source)+" ]")
	return err
}
func (m *Manager) fileSize(ctx context.Context, serial, remote string) (int64, error) {
	output, err := m.shell(ctx, serial, "[ -f "+shellQuote(remote)+" ] && [ ! -L "+shellQuote(remote)+" ] && stat -c %s -- "+shellQuote(remote))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(output), 10, 64)
}
func (m *Manager) Upload(ctx context.Context, serial, scope, parent, local string, progress func(int64, int64)) error {
	src, err := os.Open(local)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("请选择普通文件")
	}
	target, err := phone.Child(parent, filepath.Base(local))
	if err != nil {
		return err
	}
	if _, err = m.shell(ctx, serial, missingScript(target)); err != nil {
		return errors.New("目标已存在或目录不可访问，请使用其他文件名")
	}
	// Use a private staging directory so a pre-existing symlink cannot redirect writes.
	tempDir := path.Join(phone.CleanPath(parent), ".goscrcpy-"+randomID())
	if _, err = m.shell(ctx, serial, "mkdir -- "+shellQuote(tempDir)); err != nil {
		return err
	}
	temp := path.Join(tempDir, "data")
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = m.shell(cleanup, serial, "rm -f -- "+shellQuote(temp)+"; rmdir -- "+shellQuote(tempDir))
	}()
	client, err := m.client()
	if err != nil {
		return err
	}
	conn, err := connectSync(ctx, client, serial)
	if err != nil {
		return err
	}
	err = send(ctx, conn, temp, src, info.Size(), func(n int64) { progress(n, info.Size()) })
	_ = conn.Close()
	if err != nil {
		return err
	}
	size, err := m.fileSize(ctx, serial, temp)
	if err != nil {
		return err
	}
	if size != info.Size() {
		return errors.New("上传大小校验失败")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	_, err = m.shell(ctx, serial, missingScript(target)+" && mv -n -- "+shellQuote(temp)+" "+shellQuote(target)+" && [ ! -e "+shellQuote(temp)+" ]")
	return err
}
func (m *Manager) Download(ctx context.Context, serial, scope, remote, local string, progress func(int64, int64)) error {
	remote, err := clean(remote)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(local); err == nil {
		return errors.New("电脑已有同名文件，请选择其他文件名")
	} else if !os.IsNotExist(err) {
		return err
	}
	size, err := m.fileSize(ctx, serial, remote)
	if err != nil {
		return err
	}
	if size < 0 {
		return errors.New("无效文件大小")
	}
	temp, err := os.CreateTemp(filepath.Dir(local), ".goscrcpy-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	client, err := m.client()
	if err != nil {
		return err
	}
	conn, err := connectSync(ctx, client, serial)
	if err != nil {
		return err
	}
	defer conn.Close()
	done, err := receive(ctx, conn, remote, temp, func(n int64) { progress(n, size) })
	if err != nil {
		return err
	}
	if done != size {
		return errors.New("手机文件大小发生变化，请重试")
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return phone.PublishFile(temp.Name(), local)
}
