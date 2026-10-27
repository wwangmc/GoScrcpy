package phone

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"phonescreen/internal/afc"
	"phonescreen/internal/usbmux"
)

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Entry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Size       int64  `json:"size"`
	LinkTarget string `json:"linkTarget"`
	Error      string `json:"error,omitempty"`
}
type Listing struct {
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	ElapsedMS int64   `json:"elapsedMS"`
}
type SharedApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Progress struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

// One AFC request stream is serialized. Different services never share a client.
type Manager struct {
	mu            sync.Mutex
	client        *afc.Client
	connection    net.Conn
	serial, scope string
}

// Reset the socket deadline on each read/write, including long transfers.
type timedConnection struct{ net.Conn }

func (c *timedConnection) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	return c.Conn.Read(p)
}
func (c *timedConnection) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return c.Conn.Write(p)
}

func (m *Manager) closeLocked() {
	if m.client != nil {
		_ = m.client.Close()
	}
	m.client = nil
	m.connection = nil
	m.serial = ""
	m.scope = ""
}
func (m *Manager) Close() { m.mu.Lock(); defer m.mu.Unlock(); m.closeLocked() }

func (m *Manager) openLocked(ctx context.Context, serial, scope string) error {
	if m.client != nil && m.serial == serial && m.scope == scope {
		return nil
	}
	m.closeLocked()
	d, err := findDevice(ctx, serial)
	if err != nil {
		return err
	}
	service := "com.apple.afc2"
	switch {
	case scope == "media":
		service = "com.apple.afc"
	case strings.HasPrefix(scope, "app:"):
		service = "com.apple.mobile.house_arrest"
	case scope != "root":
		return errors.New("未知访问范围")
	}
	raw, err := connectService(ctx, usbmux.NewClient(), d, service)
	if err != nil {
		return fmt.Errorf("连接失败，请解锁并信任此电脑，确认对应服务已启用：%w", err)
	}
	connection := &timedConnection{raw}
	if strings.HasPrefix(scope, "app:") {
		stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
		_, e := plistRequest(connection, map[string]any{"Command": "VendDocuments", "Identifier": strings.TrimPrefix(scope, "app:")})
		if !stop() {
			e = ctx.Err()
		}
		if e != nil {
			_ = connection.Close()
			if strings.Contains(e.Error(), "InstallationLookupFailed") {
				return errors.New("此应用未向 iOS 开放共享文档。可切换到媒体目录，或在根目录中查找应用文件。")
			}
			return e
		}
	}
	m.connection = connection
	m.client = afc.NewFromConn(connection)
	m.serial = serial
	m.scope = scope
	return nil
}

func (m *Manager) withClient(ctx context.Context, serial, scope string, fn func(*afc.Client, io.ReadWriter) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.openLocked(ctx, serial, scope); err != nil {
		return err
	}
	raw := m.connection
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	err := fn(m.client, raw)
	if !stop() {
		<-ctx.Done()
		err = ctx.Err()
	}
	if err != nil {
		m.closeLocked()
	}
	return err
}

func CleanPath(p string) string { return path.Clean("/" + strings.TrimLeft(p, "/")) }

// Deletion requires an exact absolute entry path. Never normalize an ambiguous
// path into a different deletion target, or allow deletion of a scope root.
func DeletePath(p string) (string, error) {
	if p == "/" || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") || p != CleanPath(p) {
		return "", errors.New("删除路径无效，不能删除根目录")
	}
	return p, nil
}

func (m *Manager) Delete(ctx context.Context, serial, scope, remote string) error {
	remote, err := DeletePath(remote)
	if err != nil {
		return err
	}
	return m.withClient(ctx, serial, scope, func(c *afc.Client, _ io.ReadWriter) error {
		info, err := c.Stat(remote)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return c.RemoveAll(remote)
		}
		// A symlink is removed itself; its target must never be traversed.
		return c.Remove(remote)
	})
}

func Child(parent, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || strings.ContainsRune(parent, 0) {
		return "", errors.New("名称不能为空，也不能包含路径分隔符")
	}
	return path.Join(CleanPath(parent), name), nil
}
func (m *Manager) List(ctx context.Context, serial, scope, p string) (Listing, error) {
	if strings.ContainsRune(p, 0) {
		return Listing{}, errors.New("无效文件路径")
	}
	started := time.Now()
	result := Listing{Path: CleanPath(p), Entries: make([]Entry, 0)}
	err := m.withClient(ctx, serial, scope, func(c *afc.Client, _ io.ReadWriter) error {
		names, err := c.List(result.Path)
		if err != nil {
			return err
		}
		for _, name := range names {
			full, err := Child(result.Path, name)
			if err != nil {
				continue
			}
			info, err := c.Stat(full)
			item := Entry{Name: name, Path: full, Kind: "other"}
			if err != nil {
				// A directory may contain individual entries that are inaccessible.
				if strings.Contains(err.Error(), "PermDenied") || strings.Contains(err.Error(), "ObjectNotFound") {
					item.Error = "无法读取属性"
					result.Entries = append(result.Entries, item)
					continue
				}
				return err
			}
			item.Size = info.Size
			item.LinkTarget = info.LinkTarget
			switch info.Type {
			case afc.S_IFDIR:
				item.Kind = "directory"
			case afc.S_IFLNK:
				item.Kind = "link"
			default:
				if string(info.Type) == "S_IFREG" {
					item.Kind = "file"
				}
			}
			result.Entries = append(result.Entries, item)
		}
		return nil
	})
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, err
}

func requireMissing(c *afc.Client, p string) error {
	names, err := c.List(path.Dir(p))
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == path.Base(p) {
			return errors.New("目标已存在，请使用其他名称")
		}
	}
	return nil
}
func (m *Manager) Mkdir(ctx context.Context, serial, scope, parent, name string) error {
	full, err := Child(parent, name)
	if err != nil {
		return err
	}
	return m.withClient(ctx, serial, scope, func(c *afc.Client, _ io.ReadWriter) error {
		if e := requireMissing(c, full); e != nil {
			return e
		}
		return c.MkDir(full)
	})
}
func (m *Manager) Rename(ctx context.Context, serial, scope, source, name string) error {
	if strings.ContainsRune(source, 0) {
		return errors.New("无效文件路径")
	}
	source = CleanPath(source)
	if source == "/" {
		return errors.New("不能重命名根目录")
	}
	target, err := Child(path.Dir(source), name)
	if err != nil {
		return err
	}
	if target == source {
		return nil
	}
	return m.withClient(ctx, serial, scope, func(c *afc.Client, rw io.ReadWriter) error {
		if e := requireMissing(c, target); e != nil {
			return e
		}
		return renameAFC(rw, source, target)
	})
}

// go-ios exposes AFC file operations but not RENAME_PATH; add this single opcode.
func renameAFC(rw io.ReadWriter, source, target string) error {
	payload := []byte(source + "\x00" + target + "\x00")
	header := make([]byte, 40)
	copy(header, []byte("CFA6LPAA"))
	binary.LittleEndian.PutUint64(header[8:], uint64(40+len(payload)))
	binary.LittleEndian.PutUint64(header[16:], uint64(40+len(payload)))
	const sequence = uint64(0x7000000000000000)
	binary.LittleEndian.PutUint64(header[24:], sequence)
	binary.LittleEndian.PutUint64(header[32:], 0x18)
	if _, err := io.Copy(rw, strings.NewReader(string(header)+string(payload))); err != nil {
		return err
	}
	response := make([]byte, 40)
	if _, err := io.ReadFull(rw, response); err != nil {
		return err
	}
	entire := binary.LittleEndian.Uint64(response[8:])
	this := binary.LittleEndian.Uint64(response[16:])
	if string(response[:8]) != "CFA6LPAA" || entire < 40 || entire > 1024 || this < 40 || this > entire || binary.LittleEndian.Uint64(response[24:]) != sequence || binary.LittleEndian.Uint64(response[32:]) != 1 {
		return errors.New("AFC 重命名响应无效")
	}
	body := make([]byte, entire-40)
	if _, err := io.ReadFull(rw, body); err != nil {
		return err
	}
	if len(body) < 8 {
		return errors.New("AFC 状态响应不完整")
	}
	status := binary.LittleEndian.Uint64(body)
	if status != 0 {
		return fmt.Errorf("AFC 重命名失败，错误码 %d", status)
	}
	return nil
}

func randomName() string {
	var b [12]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic(err)
	}
	return ".iphonefiles-" + hex.EncodeToString(b[:]) + ".part"
}
func stream(ctx context.Context, dst io.Writer, src io.Reader, total int64, progress func(int64)) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var done int64
	for done < total {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		n, err := src.Read(buffer[:min(int64(len(buffer)), total-done)])
		if n > 0 {
			written, e := dst.Write(buffer[:n])
			done += int64(written)
			progress(done)
			if e != nil {
				return done, e
			}
			if written != n {
				return done, io.ErrShortWrite
			}
		}
		if err != nil {
			if err == io.EOF && done == total {
				return done, nil
			}
			return done, err
		}
		if n == 0 {
			return done, io.ErrNoProgress
		}
	}
	return done, nil
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
	target, err := Child(parent, filepath.Base(local))
	if err != nil {
		return err
	}
	var temp string
	committed, cleaned := false, false
	err = m.withClient(ctx, serial, scope, func(c *afc.Client, rw io.ReadWriter) error {
		if e := requireMissing(c, target); e != nil {
			return e
		}
		temp = path.Join(CleanPath(parent), randomName())
		defer func() {
			if !committed {
				cleaned = c.Remove(temp) == nil
			}
		}()
		out, e := c.Open(temp, afc.WRITE_ONLY_CREATE_TRUNC)
		if e != nil {
			return e
		}
		_, e = stream(ctx, out, src, info.Size(), func(n int64) { progress(n, info.Size()) })
		closeErr := out.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		check, e := c.Stat(temp)
		if e != nil {
			return e
		}
		if check.Size != info.Size() {
			return errors.New("上传大小校验失败")
		}
		if e = requireMissing(c, target); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = renameAFC(rw, temp, target); e != nil {
			return e
		}
		committed = true
		progress(info.Size(), info.Size())
		return nil
	})
	if !committed && !cleaned && temp != "" {
		// Cancellation closes the original socket. Reconnect solely to remove our
		// random temporary path, without reusing the cancelled context.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.withClient(cleanupCtx, serial, scope, func(c *afc.Client, _ io.ReadWriter) error {
			return c.Remove(temp)
		})
	}
	if committed {
		return nil
	}
	return err
}

func (m *Manager) Download(ctx context.Context, serial, scope, remote, local string, progress func(int64, int64)) error {
	if strings.ContainsRune(remote, 0) {
		return errors.New("无效文件路径")
	}
	if _, err := os.Lstat(local); err == nil {
		return errors.New("本地已有同名文件，请选择其他文件名")
	} else if !os.IsNotExist(err) {
		return err
	}
	committed := false
	err := m.withClient(ctx, serial, scope, func(c *afc.Client, _ io.ReadWriter) error {
		info, e := c.Stat(CleanPath(remote))
		if e != nil {
			return e
		}
		if string(info.Type) != "S_IFREG" {
			return errors.New("请选择普通文件；暂不支持整目录下载")
		}
		input, e := c.Open(CleanPath(remote), afc.READ_ONLY)
		if e != nil {
			return e
		}
		defer input.Close()
		temp, e := os.CreateTemp(filepath.Dir(local), ".iphonefiles-*.part")
		if e != nil {
			return e
		}
		tempName := temp.Name()
		defer os.Remove(tempName)
		_, e = stream(ctx, temp, input, info.Size, func(n int64) { progress(n, info.Size) })
		if e == nil {
			e = temp.Sync()
		}
		closeErr := temp.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = publishFile(tempName, local); e != nil {
			return e
		}
		committed = true
		progress(info.Size, info.Size)
		return nil
	})
	if committed {
		return nil
	}
	return err
}
