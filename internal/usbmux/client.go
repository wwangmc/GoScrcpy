// Package usbmux opens device TCP ports over USB, the same tunnel used by iproxy.
// No device IP or Wi-Fi connection is used, and no extra local listening port is needed.
package usbmux

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"time"

	"howett.net/plist"
)

type Device struct {
	ID         uint32 `plist:"DeviceID"`
	Properties struct {
		Serial         string `plist:"SerialNumber"`
		ConnectionType string `plist:"ConnectionType"`
	} `plist:"Properties"`
}

type Client struct {
	Network, Address string
}

func NewClient() *Client {
	if runtime.GOOS == "windows" {
		return &Client{"tcp", "127.0.0.1:27015"}
	}
	return &Client{"unix", "/var/run/usbmuxd"}
}

// PairRecord reads the existing trust record from the local Apple USB service.
// Records contain private keys and must never be logged or sent to the frontend.
func (c *Client) PairRecord(ctx context.Context, serial string) (map[string]any, error) {
	conn, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var reply struct {
		Data   []byte `plist:"PairRecordData"`
		Number uint64 `plist:"Number"`
	}
	if err = exchange(ctx, conn, map[string]any{"MessageType": "ReadPairRecord", "PairRecordID": serial}, &reply); err != nil {
		return nil, err
	}
	if len(reply.Data) == 0 {
		return nil, fmt.Errorf("请解锁 iPhone 并信任此电脑（配对记录不可用，代码 %d）", reply.Number)
	}
	var record map[string]any
	_, err = plist.Unmarshal(reply.Data, &record)
	return record, err
}

func (c *Client) open(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, c.Network, c.Address)
	if err != nil {
		return nil, fmt.Errorf("无法连接 USB 设备服务，请检查 Apple Mobile Device Service / usbmuxd: %w", err)
	}
	return conn, nil
}

func exchange(ctx context.Context, conn net.Conn, request map[string]any, response any) error {
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	request["ClientVersionString"] = "GoScrcpy 0.2"
	request["ProgName"] = "GoScrcpy"
	request["kLibUSBMuxVersion"] = uint64(3)
	data, err := plist.Marshal(request, plist.XMLFormat)
	if err != nil {
		return err
	}
	packet := make([]byte, 16+len(data))
	binary.LittleEndian.PutUint32(packet, uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[4:], 1)
	binary.LittleEndian.PutUint32(packet[8:], 8)
	binary.LittleEndian.PutUint32(packet[12:], 1)
	copy(packet[16:], data)
	if _, err = io.Copy(conn, bytes.NewReader(packet)); err != nil {
		return err
	}
	header := make([]byte, 16)
	if _, err = io.ReadFull(conn, header); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(header)
	if n < 16 || n > 1024*1024 {
		return fmt.Errorf("无效 USBMux 响应长度: %d", n)
	}
	if binary.LittleEndian.Uint32(header[4:]) != 1 || binary.LittleEndian.Uint32(header[8:]) != 8 || binary.LittleEndian.Uint32(header[12:]) != 1 {
		return fmt.Errorf("不支持的 USBMux 响应头")
	}
	data = make([]byte, int(n)-16)
	if _, err = io.ReadFull(conn, data); err != nil {
		return err
	}
	_, err = plist.Unmarshal(data, response)
	if err == nil {
		_ = conn.SetDeadline(time.Time{})
	}
	return err
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	conn, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var reply struct {
		Devices     []Device `plist:"DeviceList"`
		MessageType string   `plist:"MessageType"`
		Number      uint64   `plist:"Number"`
	}
	if err = exchange(ctx, conn, map[string]any{"MessageType": "ListDevices"}, &reply); err != nil {
		return nil, err
	}
	if reply.MessageType == "Result" {
		return nil, fmt.Errorf("USBMux 设备枚举失败，代码 %d", reply.Number)
	}
	devices := make([]Device, 0)
	for _, d := range reply.Devices {
		if d.Properties.ConnectionType == "USB" && d.Properties.Serial != "" {
			devices = append(devices, d)
		}
	}
	return devices, nil
}

func (c *Client) Connect(ctx context.Context, device Device, port uint16) (net.Conn, error) {
	// Device IDs are chosen from a USB-only enumeration. Reject accidental Wi-Fi entries.
	if device.Properties.ConnectionType != "USB" {
		return nil, fmt.Errorf("仅允许 USB 设备连接")
	}
	conn, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var reply struct {
		MessageType string `plist:"MessageType"`
		Number      uint64 `plist:"Number"`
	}
	err = exchange(ctx, conn, map[string]any{"MessageType": "Connect", "DeviceID": device.ID, "PortNumber": uint64(port>>8 | port<<8)}, &reply)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if reply.MessageType != "Result" || reply.Number != 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("USB 连接手机端口 %d 失败（代码 %d），请检查信任授权及手机服务", port, reply.Number)
	}
	return conn, nil
}
