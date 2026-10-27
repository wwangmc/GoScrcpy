package phone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"phonescreen/internal/afc"
)

// PasteTarget also rejects copying/moving a directory into itself. Backends
// repeat this check with resolved paths to cover navigation through symlinks.
func PasteTarget(source, parent string) (string, error) {
	if _, err := DeletePath(source); err != nil {
		return "", err
	}
	if !strings.HasPrefix(parent, "/") || strings.ContainsAny(parent, "\\\x00") || parent != CleanPath(parent) {
		return "", errors.New("目标目录路径无效")
	}
	if parent == source || strings.HasPrefix(parent, source+"/") {
		return "", errors.New("不能将文件夹粘贴到自身或其子目录")
	}
	target, err := Child(parent, path.Base(source))
	if err != nil {
		return "", err
	}
	if target == source {
		return "", errors.New("源位置与目标位置相同，请选择其他目录")
	}
	return target, nil
}

func resolveAFCPath(c *afc.Client, p string) (string, error) {
	for hops := 0; hops < 40; hops++ {
		parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
		prefix := "/"
		changed := false
		for i, part := range parts {
			if part == "" {
				continue
			}
			prefix = path.Join(prefix, part)
			info, err := c.Stat(prefix)
			if err != nil {
				return "", err
			}
			if !info.IsLink() {
				continue
			}
			if info.LinkTarget == "" {
				return "", errors.New("无法解析符号链接")
			}
			target := info.LinkTarget
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(prefix), target)
			}
			p = path.Join(target, path.Join(parts[i+1:]...))
			changed = true
			break
		}
		if !changed {
			return p, nil
		}
	}
	return "", errors.New("符号链接层级过多或存在循环")
}

func copyAFC(ctx context.Context, c *afc.Client, source, target string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 256 {
		return errors.New("文件夹层级过深")
	}
	info, err := c.Stat(source)
	if err != nil {
		return err
	}
	if info.IsLink() {
		if info.LinkTarget == "" {
			return errors.New("无法读取符号链接的目标")
		}
		return c.Symlink(info.LinkTarget, target)
	}
	if info.IsDir() {
		names, err := c.List(source)
		if err != nil {
			return err
		}
		if err = c.MkDir(target); err != nil {
			return err
		}
		for _, name := range names {
			src, err := Child(source, name)
			if err != nil {
				return err
			}
			dst, err := Child(target, name)
			if err != nil {
				return err
			}
			if err = copyAFC(ctx, c, src, dst, depth+1); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}
	if string(info.Type) != "S_IFREG" || info.Size < 0 {
		return errors.New("仅支持复制普通文件、文件夹和符号链接")
	}
	in, err := c.Open(source, afc.READ_ONLY)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := c.Open(target, afc.WRITE_ONLY_CREATE_TRUNC)
	if err != nil {
		return err
	}
	_, err = stream(ctx, out, in, info.Size, func(int64) {})
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	check, err := c.Stat(target)
	if err == nil && check.Size != info.Size {
		err = errors.New("复制文件大小校验失败")
	}
	return err
}

func (m *Manager) Paste(ctx context.Context, serial, scope, source, parent string, move bool) error {
	target, err := PasteTarget(source, parent)
	if err != nil {
		return err
	}
	var temp string
	created, cleaned, committed := false, false, false
	err = m.withClient(ctx, serial, scope, func(c *afc.Client, rw io.ReadWriter) error {
		info, err := c.Stat(source)
		if err != nil {
			return err
		}
		if info.IsDir() {
			src, err := resolveAFCPath(c, source)
			if err != nil {
				return err
			}
			dst, err := resolveAFCPath(c, parent)
			if err != nil {
				return err
			}
			if _, err = PasteTarget(src, dst); err != nil {
				return err
			}
		}
		if err = requireMissing(c, target); err != nil {
			return err
		}
		if move {
			return renameAFC(rw, source, target)
		}
		temp = path.Join(parent, ".goscrcpy-paste-"+randomName())
		if err = requireMissing(c, temp); err != nil {
			return err
		}
		if err = c.MkDir(temp); err != nil {
			return err
		}
		created = true
		defer func() {
			if !committed {
				cleaned = c.RemoveAll(temp) == nil
			}
		}()
		data := path.Join(temp, "data")
		if err = copyAFC(ctx, c, source, data, 0); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = requireMissing(c, target); err != nil {
			return err
		}
		if err = renameAFC(rw, data, target); err != nil {
			return err
		}
		committed = true
		cleaned = c.Remove(temp) == nil
		return nil
	})
	if created && !cleaned {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.withClient(cleanup, serial, scope, func(c *afc.Client, _ io.ReadWriter) error { return c.RemoveAll(temp) })
	}
	if committed {
		return nil
	}
	return err
}
