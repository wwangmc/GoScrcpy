package phonevideo

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"phonescreen/internal/scrcpy"
)

// Called with inputMu held, including the final release during shutdown.
func (s *Session) sendInput(ctx context.Context, kind string, data any, path string, body any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.control != nil {
		deadline := time.Now().Add(2 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = s.control.SetWriteDeadline(deadline)
		return s.control.WriteJSON(map[string]any{"type": kind, "data": data})
	}
	_, err := s.api.JSON(ctx, path, body)
	return err
}

func (s *Session) touch(action string, x, y int) error {
	// A lost HTTP acknowledgement may still mean the device injected the touch.
	// Remember it before sending so shutdown can release the finger even on errors.
	s.touchX, s.touchY = x, y
	if action != "up" {
		s.touchActive = true
	}
	body := map[string]any{"type": action, "x": x, "y": y}
	err := s.sendInput(s.ctx, "touch", body, "/touch", body)
	if err == nil && action == "up" {
		s.touchActive = false
	}
	return err
}
func (s *Session) position(packet []byte, offset int) (int, int, error) {
	w, h := int(binary.BigEndian.Uint16(packet[offset+8:])), int(binary.BigEndian.Uint16(packet[offset+10:]))
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("无效输入坐标尺寸")
	}
	x, y := int(binary.BigEndian.Uint32(packet[offset:])), int(binary.BigEndian.Uint32(packet[offset+4:]))
	x = min(max(x, 0), w-1)
	y = min(max(y, 0), h-1)
	return x * s.physicalW / w, y * s.physicalH / h, nil
}

// The desktop's shared control envelope is translated into PhoneVideo's public API.
// No shell, network-address, application-management or other privileged API is used.
func (s *Session) WriteControl(packet []byte) error {
	if err := scrcpy.ValidateControl(packet); err != nil {
		return err
	}
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	switch packet[0] {
	case 0:
		if packet[1] != 0 {
			return nil
		} // PhoneVideo key endpoints already send press + release.
		code := binary.BigEndian.Uint32(packet[2:])
		path := ""
		switch code {
		case 3:
			path = "/pressHome"
		case 187:
			path = "/doublePressHome"
		case 26:
			path = "/pressPowerBtn"
		case 67:
			return s.sendInput(s.ctx, "backspace", 1, "/del?num=1", nil)
		case 21:
			return s.sendInput(s.ctx, "moveCursor", -1, "/moveCursor?num=-1", nil)
		case 22:
			return s.sendInput(s.ctx, "moveCursor", 1, "/moveCursor?num=1", nil)
		case 66:
			_, err := s.api.JSON(s.ctx, "/keypress", map[string]any{"text": "\n"})
			return err
		case 61:
			_, err := s.api.JSON(s.ctx, "/keypress", map[string]any{"text": "\t"})
			return err
		default:
			return nil // Unsupported controls are disabled by capability in the shared UI.
		}
		_, err := s.api.JSON(s.ctx, path, nil)
		return err
	case 1:
		text := string(packet[5:])
		return s.sendInput(s.ctx, "inputText", text, "/input", map[string]any{"text": text})
	case 8:
		text := string(packet[14:])
		return s.sendInput(s.ctx, "inputText", text, "/input", map[string]any{"text": text})
	case 2:
		x, y, err := s.position(packet, 10)
		if err != nil {
			return err
		}
		action := "down"
		switch packet[1] {
		case 1, 3:
			action = "up"
		case 2:
			action = "move"
		}
		return s.touch(action, x, y)
	case 3:
		if s.touchActive {
			return nil
		}
		x, y, err := s.position(packet, 1)
		if err != nil {
			return err
		}
		dx := float64(int16(binary.BigEndian.Uint16(packet[13:]))) / 2048
		dy := float64(int16(binary.BigEndian.Uint16(packet[15:]))) / 2048
		if dx == 0 && dy == 0 {
			return nil
		}
		ex := min(max(x+int(math.Round(dx*float64(s.physicalW)*0.18)), 1), s.physicalW-2)
		ey := min(max(y+int(math.Round(dy*float64(s.physicalH)*0.18)), 1), s.physicalH-2)
		if err = s.touch("down", x, y); err != nil {
			return err
		}
		for i := 1; i <= 8; i++ {
			select {
			case <-s.ctx.Done():
				return s.ctx.Err()
			case <-time.After(12 * time.Millisecond):
			}
			if err = s.touch("move", x+(ex-x)*i/8, y+(ey-y)*i/8); err != nil {
				return err
			}
		}
		return s.touch("up", ex, ey)
	}
	return nil
}
