package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"phonescreen/internal/adb"
	"phonescreen/internal/androidfiles"
	"phonescreen/internal/phone"
)

type fileSystem interface {
	List(context.Context, string, string, string) (phone.Listing, error)
	Mkdir(context.Context, string, string, string, string) error
	Rename(context.Context, string, string, string, string) error
	Upload(context.Context, string, string, string, string, func(int64, int64)) error
	Download(context.Context, string, string, string, string, func(int64, int64)) error
}
type fileService struct {
	ops      sync.Mutex
	mu       sync.Mutex
	ios      phone.Manager
	android  androidfiles.Manager
	cancel   context.CancelFunc
	progress FileTransfer
}
type FileTransfer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}
type FileListing struct {
	phone.Listing
	Platform string `json:"platform"`
	Scope    string `json:"scope"`
	Hint     string `json:"hint,omitempty"`
}

func (a *App) fileBackend(serial, scope string) (fileSystem, string, string, error) {
	if err := a.checkFileSerial(serial); err != nil {
		return nil, "", "", err
	}
	entries, usbErr := a.usbmux.Devices(a.ctx)
	for _, d := range entries {
		if d.Properties.Serial == serial {
			if scope == "" {
				scope = "root"
			}
			if scope != "root" && scope != "media" && !strings.HasPrefix(scope, "app:") {
				return nil, "", "", errors.New("未知 iOS 文件范围")
			}
			return &a.files.ios, "ios", scope, nil
		}
	}
	c, err := adb.Resolve()
	if err == nil {
		var devices []adb.Device
		devices, err = c.Devices(a.ctx)
		for _, d := range devices {
			if d.Serial == serial && d.State == "device" {
				if scope != "" && scope != "storage" {
					return nil, "", "", errors.New("未知 Android 文件范围")
				}
				return &a.files.android, "android", "storage", nil
			}
		}
	}
	if usbErr != nil && err != nil {
		return nil, "", "", fmt.Errorf("无法连接文件服务：%v；%v", usbErr, err)
	}
	return nil, "", "", errors.New("手机已断开或尚未授权，请检查 USB 连接")
}
func (a *App) lockFiles() error {
	if !a.files.ops.TryLock() {
		return errors.New("正在执行文件操作，请等待完成或取消传输")
	}
	return nil
}

func (a *App) BrowseFiles(serial, scope, remote string) (FileListing, error) {
	if err := a.lockFiles(); err != nil {
		return FileListing{}, err
	}
	defer a.files.ops.Unlock()
	backend, platform, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return FileListing{}, err
	}
	if remote == "" {
		if platform == "android" {
			remote = "/sdcard"
		} else {
			remote = "/"
		}
	}
	listing, err := backend.List(a.ctx, serial, resolved, remote)
	hint := ""
	if err != nil && platform == "ios" && scope == "" && resolved == "root" {
		resolved = "media"
		listing, err = backend.List(a.ctx, serial, resolved, "/")
		if err == nil {
			hint = "AFC2 根目录不可用，已切换到媒体目录；根目录访问需要安装 AFC2。"
		}
	}
	if err == nil {
		if prefErr := a.rememberFileLocation(serial, resolved, listing.Path); prefErr != nil {
			if hint != "" {
				hint += " "
			}
			hint += "无法记住此目录，请检查配置文件写入权限。"
		}
	}
	return FileListing{Listing: listing, Platform: platform, Scope: resolved, Hint: hint}, err
}
func (a *App) FileSharedApps(serial string) ([]phone.SharedApp, error) {
	if err := a.lockFiles(); err != nil {
		return nil, err
	}
	defer a.files.ops.Unlock()
	_, platform, _, err := a.fileBackend(serial, "")
	if err != nil {
		return nil, err
	}
	if platform != "ios" {
		return nil, errors.New("仅 iOS 支持共享应用文档")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
	defer cancel()
	return phone.SharedApps(ctx, serial)
}
func (a *App) CreateFileDirectory(serial, scope, parent, name string) error {
	if err := a.lockFiles(); err != nil {
		return err
	}
	defer a.files.ops.Unlock()
	backend, _, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return err
	}
	return backend.Mkdir(a.ctx, serial, resolved, parent, name)
}
func (a *App) RenameFile(serial, scope, remote, name string) error {
	if err := a.lockFiles(); err != nil {
		return err
	}
	defer a.files.ops.Unlock()
	backend, _, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return err
	}
	return backend.Rename(a.ctx, serial, resolved, remote, name)
}
func (a *App) GetFileTransfer() FileTransfer {
	a.files.mu.Lock()
	defer a.files.mu.Unlock()
	return a.files.progress
}
func (a *App) CancelFileTransfer() {
	a.files.mu.Lock()
	defer a.files.mu.Unlock()
	if a.files.cancel != nil {
		a.files.cancel()
	}
}

func (a *App) transferFile(name, direction string, fn func(context.Context, func(int64, int64)) error) error {
	ctx, cancel := context.WithCancel(a.ctx)
	a.files.mu.Lock()
	a.files.cancel = cancel
	a.files.progress = FileTransfer{ID: randomID(), Name: name, Direction: direction, State: "running"}
	a.files.mu.Unlock()
	defer cancel()
	err := fn(ctx, func(done, total int64) {
		a.files.mu.Lock()
		a.files.progress.Done = done
		a.files.progress.Total = total
		a.files.mu.Unlock()
	})
	a.files.mu.Lock()
	defer a.files.mu.Unlock()
	a.files.cancel = nil
	a.files.progress.State = "done"
	if err != nil {
		if ctx.Err() != nil {
			a.files.progress.State = "cancelled"
			err = errors.New("传输已取消")
		} else {
			a.files.progress.State = "error"
		}
		a.files.progress.Error = err.Error()
	}
	return err
}
func (a *App) UploadFile(serial, scope, parent string) (bool, error) {
	if err := a.lockFiles(); err != nil {
		return false, err
	}
	defer a.files.ops.Unlock()
	backend, _, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return false, err
	}
	if a.wailsContext == nil {
		return false, errors.New("请在桌面程序中选择电脑文件")
	}
	local, err := wruntime.OpenFileDialog(a.wailsContext, wruntime.OpenDialogOptions{Title: "选择上传到手机当前目录的文件"})
	if err != nil || local == "" {
		return false, err
	}
	err = a.transferFile(filepath.Base(local), "upload", func(ctx context.Context, progress func(int64, int64)) error {
		return backend.Upload(ctx, serial, resolved, parent, local, progress)
	})
	return true, err
}

// UploadDroppedFiles is a native-only binding. It is deliberately absent from
// the HTTP dispatcher: a browser must never supply paths to computer files.
func (a *App) UploadDroppedFiles(serial, scope, parent string, paths []string) (int, error) {
	if a.wailsContext == nil {
		return 0, errors.New("请在 GoScrcpy 桌面窗口内拖入电脑文件")
	}
	if err := a.lockFiles(); err != nil {
		return 0, err
	}
	defer a.files.ops.Unlock()
	backend, platform, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return 0, err
	}
	if parent == "" {
		if platform == "android" {
			parent = "/sdcard/Download"
		} else {
			resolved = "media"
			parent = "/"
		}
	}
	return a.uploadLocalFiles(backend, serial, resolved, parent, paths)
}

func (a *App) uploadLocalFiles(backend fileSystem, serial, scope, parent string, paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, errors.New("没有收到可上传的文件")
	}
	// Preflight the complete drop before writing anything. Directories and
	// duplicate target names must not cause a partially submitted batch.
	type source struct {
		path, name string
		size       int64
	}
	sources := make([]source, 0, len(paths))
	names := map[string]bool{}
	var total int64
	for _, local := range paths {
		if !filepath.IsAbs(local) {
			return 0, errors.New("无法取得拖入文件的完整路径")
		}
		info, err := os.Stat(local)
		if err != nil {
			return 0, fmt.Errorf("无法读取 %s：%w", filepath.Base(local), err)
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("%s 不是普通文件；请拖入文件，暂不支持整个文件夹", info.Name())
		}
		if _, err = phone.Child(parent, info.Name()); err != nil {
			return 0, err
		}
		if names[info.Name()] {
			return 0, fmt.Errorf("拖入的文件有同名目标 %s，请分开上传或先重命名", info.Name())
		}
		names[info.Name()] = true
		if info.Size() > int64(^uint64(0)>>1)-total {
			return 0, errors.New("文件总大小超出支持范围")
		}
		total += info.Size()
		sources = append(sources, source{local, info.Name(), info.Size()})
	}
	completed := 0
	err := a.transferFile(sources[0].name, "upload", func(ctx context.Context, progress func(int64, int64)) error {
		var base int64
		progress(0, total)
		listing, err := backend.List(ctx, serial, scope, parent)
		if err != nil {
			return err
		}
		for _, entry := range listing.Entries {
			if names[entry.Name] {
				return fmt.Errorf("手机当前目录已有 %s，请先重命名后再上传", entry.Name)
			}
		}
		for index, src := range sources {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := src.name
			if len(sources) > 1 {
				name = fmt.Sprintf("%s (%d/%d)", name, index+1, len(sources))
			}
			a.files.mu.Lock()
			a.files.progress.Name = name
			a.files.mu.Unlock()
			err := backend.Upload(ctx, serial, scope, parent, src.path, func(done, _ int64) { progress(base+done, total) })
			if err != nil {
				return fmt.Errorf("已上传 %d/%d 个文件；%s：%w", completed, len(sources), src.name, err)
			}
			completed++
			base += src.size
			progress(base, total)
		}
		return nil
	})
	return completed, err
}
func safeDownloadName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, " .")
	if name == "" {
		return "download"
	}
	return name
}
func (a *App) DownloadFile(serial, scope, remote string) (bool, error) {
	if err := a.lockFiles(); err != nil {
		return false, err
	}
	defer a.files.ops.Unlock()
	backend, _, resolved, err := a.fileBackend(serial, scope)
	if err != nil {
		return false, err
	}
	if a.wailsContext == nil {
		return false, errors.New("请在桌面程序中选择电脑保存位置")
	}
	local, err := wruntime.SaveFileDialog(a.wailsContext, wruntime.SaveDialogOptions{Title: "将手机文件另存到电脑（请选择新文件名）", DefaultFilename: safeDownloadName(path.Base(remote))})
	if err != nil || local == "" {
		return false, err
	}
	err = a.transferFile(path.Base(remote), "download", func(ctx context.Context, progress func(int64, int64)) error {
		return backend.Download(ctx, serial, resolved, remote, local, progress)
	})
	return true, err
}
