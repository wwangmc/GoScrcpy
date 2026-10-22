// Package androidfiles transfers files over the ADB sync service. File contents
// stay in Go and are streamed in bounded chunks, independently of mirroring.
package androidfiles

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"phonescreen/internal/adb"
)

type syncConn struct {
	net.Conn
	stop func() bool
}

func (c *syncConn) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.Conn.Close()
}
func (c *syncConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
	return c.Conn.Read(b)
}
func (c *syncConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return c.Conn.Write(b)
}

func adbRequest(conn io.ReadWriter, service string) error {
	if len(service) > 65535 {
		return errors.New("ADB 请求过长")
	}
	if _, err := io.Copy(conn, strings.NewReader(fmt.Sprintf("%04x%s", len(service), service))); err != nil {
		return err
	}
	var status [4]byte
	if _, err := io.ReadFull(conn, status[:]); err != nil {
		return err
	}
	if string(status[:]) == "OKAY" {
		return nil
	}
	if string(status[:]) != "FAIL" {
		return errors.New("无效 ADB 服务响应")
	}
	if _, err := io.ReadFull(conn, status[:]); err != nil {
		return err
	}
	n, err := strconv.ParseUint(string(status[:]), 16, 16)
	if err != nil {
		return err
	}
	message := make([]byte, n)
	if _, err = io.ReadFull(conn, message); err != nil {
		return err
	}
	return fmt.Errorf("ADB：%s", message)
}

func connectSync(ctx context.Context, client adb.Client, serial string) (*syncConn, error) {
	if _, err := client.Run(ctx, "start-server"); err != nil {
		return nil, err
	}
	address := "127.0.0.1:5037"
	if port := os.Getenv("ANDROID_ADB_SERVER_PORT"); port != "" {
		address = net.JoinHostPort("127.0.0.1", port)
	}
	if socket := os.Getenv("ADB_SERVER_SOCKET"); socket != "" {
		if !strings.HasPrefix(socket, "tcp:") {
			return nil, errors.New("文件传输需要 TCP ADB 服务")
		}
		address = strings.TrimPrefix(socket, "tcp:")
	}
	raw, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	c := &syncConn{Conn: raw}
	c.stop = context.AfterFunc(ctx, func() { _ = raw.Close() })
	if err = adbRequest(c, "host:transport:"+serial); err == nil {
		err = adbRequest(c, "sync:")
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func syncWrite(w io.Writer, id string, payload []byte) error {
	header := make([]byte, 8)
	copy(header, id)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(payload)))
	_, err := io.Copy(w, io.MultiReader(strings.NewReader(string(header)), strings.NewReader(string(payload))))
	return err
}
func syncHeader(r io.Reader) (string, uint32, error) {
	var b [8]byte
	_, err := io.ReadFull(r, b[:])
	return string(b[:4]), binary.LittleEndian.Uint32(b[4:]), err
}
func syncFailure(r io.Reader, n uint32) error {
	if n > 65536 {
		return errors.New("无效 ADB 错误响应长度")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return fmt.Errorf("手机文件操作失败：%s", b)
}

func receive(ctx context.Context, c *syncConn, remote string, dst io.Writer, progress func(int64)) (int64, error) {
	if err := syncWrite(c, "RECV", []byte(remote)); err != nil {
		return 0, err
	}
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		id, n, err := syncHeader(c)
		if err != nil {
			return done, err
		}
		if id == "DONE" {
			return done, nil
		}
		if id == "FAIL" {
			return done, syncFailure(c, n)
		}
		if id != "DATA" || n > 64*1024 {
			return done, errors.New("无效 ADB 文件数据块")
		}
		copied, err := io.CopyN(dst, c, int64(n))
		done += copied
		progress(done)
		if err != nil {
			return done, err
		}
	}
}
func send(ctx context.Context, c *syncConn, remote string, src io.Reader, total int64, progress func(int64)) error {
	if err := syncWrite(c, "SEND", []byte(remote+",33206")); err != nil {
		return err
	} // regular file, 0666
	buffer := make([]byte, 64*1024)
	var done int64
	for done < total {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(src, buffer[:min(int64(len(buffer)), total-done)])
		if err != nil {
			return err
		}
		if err = syncWrite(c, "DATA", buffer[:n]); err != nil {
			return err
		}
		done += int64(n)
		progress(done)
	}
	var end [8]byte
	copy(end[:], "DONE")
	binary.LittleEndian.PutUint32(end[4:], uint32(time.Now().Unix()))
	if _, err := io.Copy(c, strings.NewReader(string(end[:]))); err != nil {
		return err
	}
	id, n, err := syncHeader(c)
	if err != nil {
		return err
	}
	if id == "FAIL" {
		return syncFailure(c, n)
	}
	if id != "OKAY" {
		return errors.New("ADB 未确认文件接收")
	}
	return nil
}
