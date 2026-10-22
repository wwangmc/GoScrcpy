package scrcpy

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"testing"
)

func TestMetadataAndFrameBoundaries(t *testing.T) {
	header := make([]byte, 76)
	copy(header, "Pixel 2")
	copy(header[64:], "h264")
	binary.BigEndian.PutUint32(header[68:], 1080)
	binary.BigEndian.PutUint32(header[72:], 1920)
	metadata, err := ReadMetadata(bytes.NewReader(header))
	if err != nil || metadata.Name != "Pixel 2" || metadata.Width != 1080 || metadata.Height != 1920 {
		t.Fatalf("%+v %v", metadata, err)
	}
	packet := make([]byte, 16)
	packet[0] = 0x40
	binary.BigEndian.PutUint32(packet[8:], 4)
	copy(packet[12:], []byte{0, 0, 0, 1})
	stream := bytes.NewReader(append(append([]byte{}, packet...), packet...))
	for i := 0; i < 2; i++ {
		got, err := ReadPacket(stream)
		if err != nil || !bytes.Equal(got, packet) {
			t.Fatalf("packet %d: %x %v", i, got, err)
		}
	}
	if _, err := ReadPacket(stream); err != io.EOF {
		t.Fatalf("expected EOF: %v", err)
	}
	if _, err := ReadPacket(bytes.NewReader(packet[:14])); err == nil {
		t.Fatal("truncated payload accepted")
	}
	binary.BigEndian.PutUint32(packet[8:], MaxPacket+1)
	if _, err := ReadPacket(bytes.NewReader(packet)); err == nil {
		t.Fatal("oversized packet accepted")
	}
	header[64] = 'x'
	if _, err := ReadMetadata(bytes.NewReader(header)); err == nil {
		t.Fatal("wrong codec accepted")
	}
}

func TestControlProtocol(t *testing.T) {
	// Wire fixtures matching scrcpy v3.3.4 control_msg.c, including its 32-byte touch format.
	fixtures := []string{
		"0000000000030000000000000000",
		"0100000003616263",
		"0200fffffffffffffffe00000064000000c804380780ffff0000000000000000",
		"0300000064000000c8043807800000f80000000000",
		"0800000000000000000100000003616263",
	}
	for _, fixture := range fixtures {
		packet, err := hex.DecodeString(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateControl(packet); err != nil {
			t.Errorf("%s: %v", fixture, err)
		}
		if err = ValidateControl(packet[:len(packet)-1]); err == nil {
			t.Errorf("truncated fixture accepted: %s", fixture)
		}
	}
	for _, packet := range [][]byte{nil, {255}, {1, 0, 0, 0, 1, 0xff}, {2, 0}} {
		if err := ValidateControl(packet); err == nil {
			t.Errorf("invalid accepted: %x", packet)
		}
	}
}
