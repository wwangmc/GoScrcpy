package mobileshell

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"phonescreen/internal/usbmux/muxtest"
)

func TestInteractiveSSHOverUSB(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() != "mobile" || string(password) != "test-password" {
			t.Error("wrong SSH login")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type dimensions struct{ Cols, Rows uint32 }
	pty := make(chan dimensions, 1)
	resized := make(chan dimensions, 1)
	received := make(chan string, 1)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		server, channels, requests, err := ssh.NewServerConn(conn, config)
		if err != nil {
			_ = conn.Close()
			serverDone <- err
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		for incoming := range channels {
			channel, requests, err := incoming.Accept()
			if err != nil {
				serverDone <- err
				return
			}
			for request := range requests {
				switch request.Type {
				case "pty-req":
					var req struct {
						Term                      string
						Cols, Rows, Width, Height uint32
						Modes                     string
					}
					_ = ssh.Unmarshal(request.Payload, &req)
					pty <- dimensions{req.Cols, req.Rows}
					_ = request.Reply(true, nil)
				case "window-change":
					var req struct{ Cols, Rows, Width, Height uint32 }
					_ = ssh.Unmarshal(request.Payload, &req)
					resized <- dimensions{req.Cols, req.Rows}
				case "shell":
					_ = request.Reply(true, nil)
					go func() {
						data, err := io.ReadAll(channel)
						received <- string(data)
						_, _ = channel.Write([]byte("phone terminal output\r\n"))
						_, _ = channel.Stderr().Write([]byte("phone terminal stderr\r\n"))
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{0}))
						_ = channel.Close()
						serverDone <- err
					}()
				}
			}
			return
		}
	}()
	mux := muxtest.StartPorts(t, map[uint32]map[uint16]string{1: {22: listener.Addr().String()}})
	devices, err := mux.Client.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := mux.Client.Connect(context.Background(), devices[0], 22)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var output bytes.Buffer
	trusted, rawPrepared := false, false
	clientConfig := &ssh.ClientConfig{User: "mobile", Auth: []ssh.AuthMethod{ssh.Password("test-password")}, HostKeyCallback: func(host string, addr net.Addr, key ssh.PublicKey) error {
		trusted = true
		return ssh.FixedHostKey(signer.PublicKey())(host, addr, key)
	}}
	session, err := OpenSSH(conn, clientConfig, 100, 30, input, &output, func() error {
		if !trusted {
			t.Error("terminal entered raw mode before trust/login")
		}
		rawPrepared = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !rawPrepared {
		t.Fatal("terminal was not prepared")
	}
	if d := <-pty; d != (dimensions{100, 30}) {
		t.Fatal("wrong PTY size", d)
	}
	if err = session.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-resized:
		if d != (dimensions{120, 40}) {
			t.Fatal("wrong resize", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resize not delivered")
	}
	payload := "echo test\r\x03exit\r"
	if _, err = writer.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err = session.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := <-received; got != payload {
		t.Fatalf("terminal control bytes changed: %q", got)
	}
	if !strings.Contains(output.String(), "phone terminal output") {
		t.Fatal("missing streaming terminal output")
	}
	if !strings.Contains(output.String(), "phone terminal stderr") {
		t.Fatal("missing streaming terminal stderr")
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}
