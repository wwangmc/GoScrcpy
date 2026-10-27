package phonevideo

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"phonescreen/internal/scrcpy"
	"phonescreen/internal/usbmux"
)

// PhoneVideoPort is on the phone. No fixed computer port or Wi-Fi is used.
const PhoneVideoPort uint16 = 27184

// Probe only the device port: do not send a start request or allocate an encoder.
// This also works while PhoneVideo is already serving another viewer.
func hasPhoneVideo(ctx context.Context, mux *usbmux.Client, d usbmux.Device) bool {
	probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	conn, err := mux.Connect(probe, d, PhoneVideoPort)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// startVideo reports absent only when USBMux cannot open the plugin's port. Once
// connected, protocol/encoder errors are surfaced instead of silently using JPEG.
func startVideo(ctx context.Context, mux *usbmux.Client, d usbmux.Device, options scrcpy.Options) (conn net.Conn, metadata scrcpy.Metadata, absent bool, err error) {
	probe, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	conn, err = mux.Connect(probe, d, PhoneVideoPort)
	cancel()
	if err != nil {
		return nil, metadata, true, nil
	}
	success := false
	raw := conn
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	var request [16]byte
	copy(request[:], "PMV1")
	binary.BigEndian.PutUint32(request[4:], uint32(min(options.MaxFPS, 60)))
	binary.BigEndian.PutUint32(request[8:], uint32(options.BitRate))
	binary.BigEndian.PutUint32(request[12:], uint32(options.MaxSize))
	if _, err = io.Copy(conn, bytes.NewReader(request[:])); err != nil {
		return nil, metadata, false, fmt.Errorf("PhoneVideo 握手失败: %w", err)
	}
	metadata, err = scrcpy.ReadMetadata(conn)
	if err != nil {
		return nil, metadata, false, fmt.Errorf("PhoneVideo 视频启动失败，请检查插件日志或是否已有投屏连接: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	return conn, metadata, false, nil
}
