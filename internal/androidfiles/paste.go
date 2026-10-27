package androidfiles

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"phonescreen/internal/phone"
)

func (m *Manager) Paste(ctx context.Context, serial, scope, source, parent string, move bool) error {
	target, err := phone.PasteTarget(source, parent)
	if err != nil {
		return err
	}
	// realpath covers /sdcard aliases and directory symlinks. A final symlink
	// is copied/moved as a link, and must not be dereferenced for this check.
	script := "s=" + shellQuote(source) + "; p=" + shellQuote(parent) + "; [ -d \"$p\" ] && { [ -e \"$s\" ] || [ -L \"$s\" ]; } || exit 1; if [ -d \"$s\" ] && [ ! -L \"$s\" ]; then s=$(realpath \"$s\") && p=$(realpath \"$p\") || exit 1; case \"$p/\" in \"$s/\"*) exit 1;; esac; fi; " + missingScript(target)
	if _, err = m.shell(ctx, serial, script); err != nil {
		return errors.New("目标已存在、目标在源文件夹内，或路径不可访问；请选择其他目录")
	}
	if move {
		_, err = m.shellFor(ctx, serial, missingScript(target)+" && mv -nT -- "+shellQuote(source)+" "+shellQuote(target)+" && "+missingScript(source), 30*time.Minute)
		return err
	}
	temp := path.Join(parent, ".goscrcpy-paste-"+randomID())
	if !strings.HasPrefix(temp, parent+"/") && parent != "/" {
		return errors.New("临时目录路径无效")
	}
	if _, err = m.shell(ctx, serial, "mkdir -- "+shellQuote(temp)); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = m.shell(cleanup, serial, "rm -r -- "+shellQuote(temp))
	}()
	data := path.Join(temp, "data")
	_, err = m.shellFor(ctx, serial, "cp -a -- "+shellQuote(source)+" "+shellQuote(data)+" && "+missingScript(target)+" && mv -nT -- "+shellQuote(data)+" "+shellQuote(target)+" && "+missingScript(data), 30*time.Minute)
	return err
}
