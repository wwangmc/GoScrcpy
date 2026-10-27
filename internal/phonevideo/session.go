package phonevideo

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux"
)

const maxImage = 16 * 1024 * 1024

type Session struct {
	serial               string
	metadata             scrcpy.Metadata
	api                  *API
	ctx                  context.Context
	cancel               context.CancelFunc
	done                 chan struct{}
	once                 sync.Once
	claimMu              sync.Mutex
	claimed              bool
	inputMu              sync.Mutex
	touchActive          bool
	touchX, touchY       int
	physicalW, physicalH int
	control              *websocket.Conn
	pending              []byte
	fps                  int
	nextFrame            time.Time
	started              time.Time
	quality              string
	video                net.Conn
}

func Start(parent context.Context, mux *usbmux.Client, serial string, options scrcpy.Options) (_ *Session, err error) {
	if err = options.Validate(); err != nil {
		return nil, err
	}
	entries, err := mux.Devices(parent)
	if err != nil {
		return nil, err
	}
	var found *usbmux.Device
	for _, d := range entries {
		if d.Properties.Serial == serial {
			entry := d
			found = &entry
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("USB 设备已断开，请重新连接并刷新")
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Session{serial: serial, api: NewAPI(mux, *found), ctx: ctx, cancel: cancel, done: make(chan struct{}), fps: min(options.MaxFPS, 60), quality: "0.1", started: time.Now()}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	s.physicalW, s.physicalH, err = s.api.ScreenSize(ctx)
	if err != nil {
		return nil, err
	}
	info, err := s.api.JSON(ctx, "/deviceInfo?type=30", nil)
	if err != nil {
		return nil, err
	}
	s.metadata.Name = strings.Split(info, ";;")[0]
	// Video and input are both supplied by PhoneVideoTweak, on separate USB channels.
	if options.VideoMode != "jpeg" {
		video, metadata, absent, videoErr := startVideo(ctx, mux, *found, options)
		if videoErr != nil {
			return nil, videoErr
		}
		if absent && options.VideoMode == "h264" {
			return nil, fmt.Errorf("未检测到 PhoneVideoTweak 视频服务，无法使用 USB H.264；请安装插件并重启 SpringBoard，或选择截图")
		}
		if !absent {
			s.video, s.metadata = video, metadata
		}
	}
	// Keep one IOSurface screenshot request in flight and never buffer old frames.
	if s.video == nil {
		s.pending, err = s.nextImage()
		if err != nil {
			return nil, fmt.Errorf("读取手机首帧: %w", err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(s.pending))
		if err != nil {
			return nil, fmt.Errorf("手机未返回有效屏幕图像: %w", err)
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 {
			return nil, fmt.Errorf("手机图像尺寸超出范围")
		}
		s.metadata.Width, s.metadata.Height = uint32(config.Width), uint32(config.Height)
	}
	// Input has its own persistent USB WebSocket and does not wait for an HTTP
	// round trip on every mouse movement. HTTP is used if the handshake fails.
	s.control, _ = s.api.ConnectControl(ctx)
	if s.control != nil {
		go func() {
			defer s.control.Close()
			for {
				if _, _, err := s.control.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}
	go func() { <-ctx.Done(); s.Close() }()
	return s, nil
}

func (s *Session) nextImage() ([]byte, error) {
	// No-change responses keep the previous picture and never become blank frames.
	for {
		if wait := time.Until(s.nextFrame); wait > 0 {
			select {
			case <-s.ctx.Done():
				return nil, s.ctx.Err()
			case <-time.After(wait):
			}
		}
		s.nextFrame = time.Now().Add(time.Second / time.Duration(s.fps))
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		path := "/screenshot?quality=" + s.quality
		r, err := s.api.request(ctx, path, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		if r.StatusCode == 204 {
			_ = r.Body.Close()
			cancel()
			if s.metadata.Width == 0 {
				// Dirty-frame state is shared by the plugin's clients. Guarantee the
				// new viewer a first image without resetting another viewer's state.
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				r, err := s.api.request(ctx, "/screenshot_original", nil)
				if err != nil {
					cancel()
					return nil, err
				}
				b, err := io.ReadAll(io.LimitReader(r.Body, maxImage+1))
				_ = r.Body.Close()
				cancel()
				if len(b) == 0 || len(b) > maxImage {
					return nil, fmt.Errorf("无效截图长度: %d", len(b))
				}
				return b, err
			}
			continue
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, maxImage+1))
		_ = r.Body.Close()
		cancel()
		if len(b) == 0 || len(b) > maxImage {
			return nil, fmt.Errorf("无效截图长度: %d", len(b))
		}
		return b, err
	}
}

func (s *Session) ReadFrame() ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if s.video != nil {
		_ = s.video.SetReadDeadline(time.Now().Add(5 * time.Second))
		return scrcpy.ReadPacket(s.video)
	}
	b := s.pending
	s.pending = nil
	if b == nil {
		var err error
		b, err = s.nextImage()
		if err != nil {
			return nil, err
		}
	}
	packet := make([]byte, 12+len(b))
	binary.BigEndian.PutUint64(packet, uint64(time.Since(s.started).Microseconds()))
	binary.BigEndian.PutUint32(packet[8:], uint32(len(b)))
	copy(packet[12:], b)
	return packet, nil
}
func (s *Session) SerialNumber() string  { return s.serial }
func (s *Session) Info() scrcpy.Metadata { return s.metadata }
func (s *Session) Encoding() string {
	if s.video != nil {
		return "h264"
	}
	return "jpeg"
}
func (s *Session) PointerIntervalMS() int { return 50 }
func supportedControls() []string {
	return []string{"touch", "scroll", "text", "keyboard", "home", "recent", "power", "key3", "key26", "key187", "key66", "key67", "key61", "key21", "key22"}
}
func (s *Session) Controls() []string { return supportedControls() }
func (s *Session) Settings() []string {
	if s.video != nil {
		return []string{"maxFPS", "maxSize", "bitRate"}
	}
	return []string{"maxFPS"}
}
func (s *Session) DoneChannel() <-chan struct{} { return s.done }
func (s *Session) Logs() string {
	control := "HTTP"
	if s.control != nil {
		control = "WebSocket"
	}
	if s.video != nil {
		return fmt.Sprintf("PhoneVideo / USBMux → device :%d / H.264 / target %d FPS / %s :%d input %s", PhoneVideoPort, s.fps, s.api.ServiceName(), s.api.ControlPort(), control)
	}
	return fmt.Sprintf("%s / USBMux → device :%d / /screenshot?quality=%s / target %d FPS / input %s", s.api.ServiceName(), s.api.ControlPort(), s.quality, s.fps, control)
}
func (s *Session) Claim() bool {
	s.claimMu.Lock()
	defer s.claimMu.Unlock()
	if s.claimed {
		return false
	}
	s.claimed = true
	return true
}

func (s *Session) Close() {
	s.once.Do(func() {
		s.inputMu.Lock()
		if s.touchActive {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			body := map[string]any{"type": "up", "x": s.touchX, "y": s.touchY}
			if err := s.sendInput(ctx, "touch", body, "/touch", body); err != nil {
				_, _ = s.api.JSON(ctx, "/touch", body)
			}
			cancel()
			s.touchActive = false
		}
		s.inputMu.Unlock()
		s.cancel()
		if s.video != nil {
			_ = s.video.Close()
		}
		if s.control != nil {
			_ = s.control.Close()
		}
		s.api.Close()
		close(s.done)
	})
}
