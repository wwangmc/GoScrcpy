package phone

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"time"

	"howett.net/plist"
	"phonescreen/internal/usbmux"
)

func findDevice(ctx context.Context, serial string) (usbmux.Device, error) {
	devices, err := usbmux.NewClient().Devices(ctx)
	if err != nil {
		return usbmux.Device{}, err
	}
	for _, d := range devices {
		if d.Properties.Serial == serial {
			return d, nil
		}
	}
	return usbmux.Device{}, errors.New("未找到所选 USB 设备，请重新连接")
}

func writePlist(w io.Writer, v any) error {
	b, err := plist.Marshal(v, plist.XMLFormat)
	if err != nil {
		return err
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(b)))
	_, err = io.Copy(w, bytes.NewReader(append(header, b...)))
	return err
}
func readPlist(r io.Reader) (map[string]any, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > 16*1024*1024 {
		return nil, errors.New("无效的 iOS 服务响应长度")
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	var reply map[string]any
	_, err := plist.Unmarshal(b, &reply)
	if err == nil && reply["Error"] != nil {
		err = fmt.Errorf("iOS 服务：%v", reply["Error"])
	}
	return reply, err
}
func plistRequest(rw io.ReadWriter, v any) (map[string]any, error) {
	if err := writePlist(rw, v); err != nil {
		return nil, err
	}
	return readPlist(rw)
}

func serviceTLS(ctx context.Context, conn net.Conn, record map[string]any) (net.Conn, error) {
	certPEM, _ := record["HostCertificate"].([]byte)
	keyPEM, _ := record["HostPrivateKey"].([]byte)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("配对证书不可用，请重新信任电脑：%w", err)
	}
	devicePEM, _ := record["DeviceCertificate"].([]byte)
	expected, _ := pem.Decode(devicePEM)
	if expected == nil {
		return nil, errors.New("配对记录缺少设备证书")
	}
	// Apple trust records identify the device by its certificate, not a DNS name.
	config := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10, InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("设备未提供配对证书")
			}
			// Pin the device certificate from the locally trusted pairing record. Old
			// Apple pairing certificates may be expired, so compare exact DER bytes.
			if bytes.Equal(rawCerts[0], expected.Bytes) {
				return nil
			}
			return errors.New("设备证书与配对记录不符")
		}}
	secure := tls.Client(conn, config)
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return secure, nil
}

func connectService(ctx context.Context, mux *usbmux.Client, device usbmux.Device, name string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	record, err := mux.PairRecord(ctx, device.Properties.Serial)
	if err != nil {
		return nil, err
	}
	lock, err := mux.Connect(ctx, device, 62078)
	if err != nil {
		return nil, err
	}
	baseLock := lock
	defer baseLock.Close()
	stop := context.AfterFunc(ctx, func() { _ = baseLock.Close() })
	defer stop()
	_ = lock.SetDeadline(time.Now().Add(20 * time.Second))
	session, err := plistRequest(lock, map[string]any{"Label": "GoScrcpy", "Request": "StartSession", "HostID": record["HostID"], "SystemBUID": record["SystemBUID"]})
	if err != nil {
		return nil, err
	}
	if enabled, _ := session["EnableSessionSSL"].(bool); enabled {
		lock, err = serviceTLS(ctx, lock, record)
		if err != nil {
			return nil, err
		}
	}
	reply, err := plistRequest(lock, map[string]any{"Label": "GoScrcpy", "Request": "StartService", "Service": name})
	if err != nil {
		return nil, fmt.Errorf("无法启动 %s：%w", name, err)
	}
	port, _ := reply["Port"].(uint64)
	if port == 0 || port > 65535 {
		return nil, errors.New("iOS 服务未返回有效端口")
	}
	conn, err := mux.Connect(ctx, device, uint16(port))
	if err != nil {
		return nil, err
	}
	if enabled, _ := reply["EnableServiceSSL"].(bool); enabled {
		secured, e := serviceTLS(ctx, conn, record)
		if e != nil {
			_ = conn.Close()
			return nil, e
		}
		conn = secured
	}
	return conn, nil
}

func SharedApps(ctx context.Context, serial string) ([]SharedApp, error) {
	d, err := findDevice(ctx, serial)
	if err != nil {
		return nil, err
	}
	conn, err := connectService(ctx, usbmux.NewClient(), d, "com.apple.mobile.installation_proxy")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err = writePlist(conn, map[string]any{"Command": "Browse", "ClientOptions": map[string]any{"ApplicationType": "Any", "ReturnAttributes": []string{"CFBundleIdentifier", "CFBundleDisplayName", "CFBundleName", "UIFileSharingEnabled"}}}); err != nil {
		return nil, err
	}
	result := make([]SharedApp, 0)
	for {
		reply, e := readPlist(conn)
		if e != nil {
			return nil, e
		}
		list, _ := reply["CurrentList"].([]any)
		for _, value := range list {
			app, ok := value.(map[string]any)
			if !ok {
				continue
			}
			enabled, _ := app["UIFileSharingEnabled"].(bool)
			if !enabled {
				continue
			}
			id, _ := app["CFBundleIdentifier"].(string)
			name, _ := app["CFBundleDisplayName"].(string)
			if name == "" {
				name, _ = app["CFBundleName"].(string)
			}
			if name == "" {
				name = id
			}
			if id != "" {
				result = append(result, SharedApp{ID: id, Name: name})
			}
		}
		if reply["Status"] == "Complete" {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
