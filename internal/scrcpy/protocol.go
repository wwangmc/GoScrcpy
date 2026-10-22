// Package scrcpy implements the pinned scrcpy 3.3.4 wire protocol.
package scrcpy

import (
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"
)

const Version = "3.3.4"
const MaxPacket = 16 * 1024 * 1024

type Metadata struct {
	Name   string `json:"name"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

func ReadMetadata(r io.Reader) (Metadata, error) {
	var data [76]byte // 64-byte device name + codec id + dimensions
	if _, err := io.ReadFull(r, data[:]); err != nil {
		return Metadata{}, err
	}
	if string(data[64:68]) != "h264" {
		return Metadata{}, fmt.Errorf("服务端未返回 H.264 视频")
	}
	n := 0
	for n < 64 && data[n] != 0 {
		n++
	}
	m := Metadata{string(data[:n]), binary.BigEndian.Uint32(data[68:72]), binary.BigEndian.Uint32(data[72:76])}
	if m.Width == 0 || m.Height == 0 || m.Width > 65535 || m.Height > 65535 {
		return Metadata{}, fmt.Errorf("无效视频尺寸 %dx%d", m.Width, m.Height)
	}
	return m, nil
}

func ReadPacket(r io.Reader) ([]byte, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[8:])
	if n == 0 || n > MaxPacket {
		return nil, fmt.Errorf("无效视频包长度: %d", n)
	}
	packet := make([]byte, 12+int(n))
	copy(packet, header[:])
	_, err := io.ReadFull(r, packet[12:])
	return packet, err
}

// ValidateControl limits the local WebSocket to the input messages this UI uses.
func ValidateControl(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("空控制包")
	}
	valid := false
	switch b[0] {
	case 0:
		valid = len(b) == 14 && b[1] <= 1
	case 1:
		valid = len(b) >= 5 && len(b) <= 305 && int(binary.BigEndian.Uint32(b[1:5])) == len(b)-5 && utf8.Valid(b[5:])
	case 2:
		valid = len(b) == 32 && b[1] <= 3 && binary.BigEndian.Uint16(b[18:20]) > 0 && binary.BigEndian.Uint16(b[20:22]) > 0
	case 3:
		valid = len(b) == 21 && binary.BigEndian.Uint16(b[9:11]) > 0 && binary.BigEndian.Uint16(b[11:13]) > 0
	case 8:
		valid = len(b) >= 14 && len(b) <= 65536 && int(binary.BigEndian.Uint32(b[10:14])) == len(b)-14 && utf8.Valid(b[14:])
	}
	if !valid {
		return fmt.Errorf("无效控制包: type=%d length=%d", b[0], len(b))
	}
	return nil
}
