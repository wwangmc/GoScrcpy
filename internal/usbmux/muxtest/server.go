// Package muxtest is an in-process USBMux daemon used by transport integration tests.
package muxtest

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"howett.net/plist"
	"phonescreen/internal/usbmux"
)

type Server struct {
	Client      *usbmux.Client
	Active      atomic.Int32
	Connections atomic.Int32
	listener    net.Listener
	mu          sync.Mutex
	sockets     map[net.Conn]bool
	closed      bool
}

func Start(t *testing.T, targets map[uint32]string) *Server {
	ports := make(map[uint32]map[uint16]string, len(targets))
	for id, address := range targets {
		ports[id] = map[uint16]string{8081: address}
	}
	return StartPorts(t, ports)
}

// StartPorts routes phone ports separately, allowing independent video and input
// connections and multiple phones without fixed host ports.
func StartPorts(t *testing.T, targets map[uint32]map[uint16]string) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Client: &usbmux.Client{Network: "tcp", Address: l.Addr().String()}, listener: l, sockets: map[net.Conn]bool{}}
	t.Cleanup(func() {
		_ = l.Close()
		s.mu.Lock()
		s.closed = true
		for c := range s.sockets {
			_ = c.Close()
		}
		s.mu.Unlock()
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				_ = conn.Close()
				return
			}
			s.sockets[conn] = true
			s.mu.Unlock()
			s.Active.Add(1)
			go s.serve(conn, targets)
		}
	}()
	return s
}

func (s *Server) serve(conn net.Conn, targets map[uint32]map[uint16]string) {
	defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.sockets, conn); s.mu.Unlock(); s.Active.Add(-1) }()
	header := make([]byte, 16)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	n := binary.LittleEndian.Uint32(header)
	if n < 16 || n > 1024*1024 {
		return
	}
	data := make([]byte, n-16)
	if _, err := io.ReadFull(conn, data); err != nil {
		return
	}
	var request struct {
		Message string `plist:"MessageType"`
		ID      uint32 `plist:"DeviceID"`
		Port    uint16 `plist:"PortNumber"`
	}
	if _, err := plist.Unmarshal(data, &request); err != nil {
		return
	}
	var reply any
	var remote net.Conn
	if request.Message == "ListDevices" {
		list := make([]any, 0)
		for id := range targets {
			list = append(list, map[string]any{"DeviceID": id, "Properties": map[string]any{"SerialNumber": serial(id), "ConnectionType": "USB"}})
		}
		list = append(list, map[string]any{"DeviceID": uint32(999), "Properties": map[string]any{"SerialNumber": "WIFI-IGNORED", "ConnectionType": "Network"}})
		reply = map[string]any{"DeviceList": list}
	} else if request.Message == "Connect" {
		code := uint64(3)
		port := request.Port>>8 | request.Port<<8
		if address, ok := targets[request.ID][port]; ok {
			var err error
			remote, err = net.Dial("tcp", address)
			if err == nil {
				code = 0
				s.Connections.Add(1)
			}
		}
		reply = map[string]any{"MessageType": "Result", "Number": code}
	} else {
		reply = map[string]any{"MessageType": "Result", "Number": uint64(1)}
	}
	data, _ = plist.Marshal(reply, plist.XMLFormat)
	binary.LittleEndian.PutUint32(header, uint32(16+len(data)))
	if _, err := conn.Write(append(header, data...)); err != nil {
		if remote != nil {
			_ = remote.Close()
		}
		return
	}
	if remote == nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(remote, conn); _ = remote.Close(); close(done) }()
	_, _ = io.Copy(conn, remote)
	_ = conn.Close()
	_ = remote.Close()
	<-done
}

func serial(id uint32) string {
	if id == 1 {
		return "USB-ONE"
	}
	return "USB-TWO"
}
