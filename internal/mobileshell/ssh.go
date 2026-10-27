// Package mobileshell provides the device-side OpenSSH terminal transport.
package mobileshell

import (
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHSession struct {
	client  *ssh.Client
	session *ssh.Session
	once    sync.Once
}

// SSH drains stdout and stderr concurrently. Serialize the shared terminal
// writer, including writers such as bytes.Buffer that are not concurrency-safe.
type terminalWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (w *terminalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.Write(p)
}

// OpenSSH uses the supplied USB connection; it never dials a phone's Wi-Fi address.
func OpenSSH(conn net.Conn, config *ssh.ClientConfig, cols, rows int, input io.Reader, output io.Writer, beforeShell func() error) (*SSHSession, error) {
	connection, channels, requests, err := ssh.NewClientConn(conn, "ios-usb", config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(connection, channels, requests)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	result := &SSHSession{client: client, session: session}
	if beforeShell != nil {
		if err = beforeShell(); err != nil {
			result.Close()
			return nil, err
		}
	}
	session.Stdin = input
	terminal := &terminalWriter{output: output}
	session.Stdout = terminal
	session.Stderr = terminal
	if err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); err != nil {
		result.Close()
		return nil, err
	}
	if err = session.Shell(); err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}
func (s *SSHSession) Wait() error                 { return s.session.Wait() }
func (s *SSHSession) Resize(cols, rows int) error { return s.session.WindowChange(rows, cols) }
func (s *SSHSession) Close()                      { s.once.Do(func() { _ = s.session.Close(); _ = s.client.Close() }) }

// The marker is interpreted only after checking the remote command's exit code.
const ProbeCommand = `if [ -x /usr/sbin/sshd ] || [ -x /var/jb/usr/sbin/sshd ] || command -v sshd >/dev/null 2>&1; then printf installed; else printf missing; fi`
