package scrcpy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"phonescreen/internal/adb"
)

type Options struct {
	MaxSize int `json:"maxSize"`
	MaxFPS  int `json:"maxFPS"`
	BitRate int `json:"bitRate"`
}

func (o Options) Validate() error {
	if o.MaxSize < 256 || o.MaxSize > 4096 || o.MaxFPS < 1 || o.MaxFPS > 120 || o.BitRate < 500000 || o.BitRate > 50000000 {
		return fmt.Errorf("投屏参数超出范围（尺寸 256–4096，帧率 1–120，码率 0.5–50 Mbps）")
	}
	return nil
}

type logBuffer struct {
	mu   sync.Mutex
	text string
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > 8192 {
		b.text = b.text[len(b.text)-8192:]
	}
	return len(p), nil
}
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.text)
}

type Session struct {
	Serial   string
	Metadata Metadata
	Video    net.Conn
	control  net.Conn
	client   adb.Client
	port     string
	cancel   context.CancelFunc
	once     sync.Once
	writeMu  sync.Mutex
	attachMu sync.Mutex
	attached bool
	logs     logBuffer
	Done     chan struct{}
}

func Start(parent context.Context, client adb.Client, serverPath, serial string, options Options) (_ *Session, err error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Session{Serial: serial, client: client, cancel: cancel, Done: make(chan struct{})}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	var random [4]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%08x", binary.BigEndian.Uint32(random[:])&0x7fffffff)
	remote := "/data/local/tmp/goscrcpy-" + id + ".jar"
	if _, err = client.Run(ctx, "-s", serial, "push", serverPath, remote); err != nil {
		return nil, err
	}
	// The server unlinks its own jar on startup. If startup fails, remove only our unique file.
	defer func() {
		if err != nil {
			_, _ = client.Run(context.Background(), "-s", serial, "shell", "rm", "-f", remote)
		}
	}()
	s.port, err = client.Run(ctx, "-s", serial, "forward", "tcp:0", "localabstract:scrcpy_"+id)
	if err != nil {
		return nil, err
	}
	portNumber, parseErr := strconv.Atoi(s.port)
	if parseErr != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("ADB 返回了无效端口: %q", s.port)
	}
	cmd := client.Command(ctx, "-s", serial, "shell", "CLASSPATH="+remote, "app_process", "/", "com.genymobile.scrcpy.Server", Version,
		"scid="+id, "log_level=info", "tunnel_forward=true", "audio=false", "control=true", "video_codec=h264",
		"clipboard_autosync=false", "max_size="+strconv.Itoa(options.MaxSize), "max_fps="+strconv.Itoa(options.MaxFPS), "video_bit_rate="+strconv.Itoa(options.BitRate))
	cmd.Stdout, cmd.Stderr = &s.logs, &s.logs
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	processDone := make(chan struct{})
	setupDone := make(chan struct{})
	defer close(setupDone)
	go func() { _ = cmd.Wait(); close(processDone); <-setupDone; s.Close() }()
	deadline := time.Now().Add(12 * time.Second)
	address := net.JoinHostPort("127.0.0.1", s.port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("投屏服务启动失败: %s", s.logs.String())
		case <-processDone:
			return nil, fmt.Errorf("投屏服务退出: %s", s.logs.String())
		default:
		}
		conn, dialErr := net.DialTimeout("tcp", address, 400*time.Millisecond)
		if dialErr == nil {
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			var dummy [1]byte
			if _, dialErr = io.ReadFull(conn, dummy[:]); dialErr == nil && dummy[0] == 0 {
				s.Video = conn
				break
			}
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.Video == nil {
		return nil, fmt.Errorf("等待手机投屏服务超时: %s", s.logs.String())
	}
	s.control, err = net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接控制通道: %w", err)
	}
	_ = s.Video.SetReadDeadline(time.Now().Add(5 * time.Second))
	s.Metadata, err = ReadMetadata(s.Video)
	if err != nil {
		return nil, fmt.Errorf("读取视频信息: %w — %s", err, s.logs.String())
	}
	_ = s.Video.SetReadDeadline(time.Time{})
	// Drain device messages so clipboard acknowledgements never block the Android sender.
	go func() { _, _ = io.Copy(io.Discard, s.control) }()
	return s, nil
}

func (s *Session) Claim() bool {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if s.attached {
		return false
	}
	s.attached = true
	return true
}

func (s *Session) WriteControl(packet []byte) error {
	if err := ValidateControl(packet); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.control.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := io.Copy(s.control, bytes.NewReader(packet))
	return err
}

func (s *Session) Logs() string { return s.logs.String() }

func (s *Session) Close() {
	s.once.Do(func() {
		s.cancel()
		if s.Video != nil {
			_ = s.Video.Close()
		}
		if s.control != nil {
			_ = s.control.Close()
		}
		if s.port != "" {
			_, _ = s.client.Run(context.Background(), "-s", s.Serial, "forward", "--remove", "tcp:"+s.port)
		}
		close(s.Done)
	})
}
