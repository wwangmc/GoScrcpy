package afc

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type testConnection struct {
	io.Reader
	bytes.Buffer
}

func (c *testConnection) Read(b []byte) (int, error) { return c.Reader.Read(b) }
func (c *testConnection) Close() error               { return nil }

func TestResponseBoundsAndSequence(t *testing.T) {
	for _, h := range []header{
		{Magic: magic, EntireLen: 39, ThisLen: 39},
		{Magic: magic, EntireLen: 40, ThisLen: 41},
		{Magic: magic, EntireLen: 1 << 60, ThisLen: 40},
		{Magic: 0, EntireLen: 40, ThisLen: 40},
		{Magic: magic, EntireLen: 40, ThisLen: 40, PacketNum: 99},
		{Magic: magic, EntireLen: 40, ThisLen: 40, Operation: status},
	} {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.LittleEndian, h)
		c := NewFromConn(&testConnection{Reader: &b})
		if _, err := c.readPacket(); err == nil {
			t.Fatalf("malformed response accepted: %+v", h)
		}
	}
}
func TestOpenRejectsMissingHandle(t *testing.T) {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, header{Magic: magic, EntireLen: 40, ThisLen: 40, PacketNum: 1, Operation: fileOpenResult})
	c := NewFromConn(&testConnection{Reader: &b})
	if _, err := c.Open("/test", READ_ONLY); err == nil {
		t.Fatal("open without file handle accepted")
	}
}
