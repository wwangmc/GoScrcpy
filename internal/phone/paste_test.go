package phone

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"phonescreen/internal/afc"
)

type afcTestNode struct{ kind, data string }
type afcTestHandle struct {
	path   string
	offset int
}

// Exercise the real AFC wire client, not a mock of the copy implementation.
func pasteFixture(t *testing.T, failWrite bool) (*Manager, map[string]afcTestNode) {
	t.Helper()
	nodes := map[string]afcTestNode{
		"/": {kind: "S_IFDIR"}, "/Documents": {kind: "S_IFDIR"}, "/Target": {kind: "S_IFDIR"}, "/Moved": {kind: "S_IFDIR"},
		"/Documents/source": {kind: "S_IFDIR"}, "/Documents/source/nested": {kind: "S_IFDIR"}, "/Documents/source/nested/file": {kind: "S_IFREG", data: "binary\x00\xffdata"},
		"/Documents/source/empty": {kind: "S_IFDIR"}, "/Documents/source/link": {kind: "S_IFLNK", data: "../keep"}, "/Documents/source/dangling": {kind: "S_IFLNK", data: "/missing"},
		"/Alias": {kind: "S_IFLNK", data: "/Documents/source/nested"}, "/Documents/keep": {kind: "S_IFREG", data: "preserve"},
	}
	host, device := net.Pipe()
	_ = host.SetDeadline(time.Now().Add(5 * time.Second))
	_ = device.SetDeadline(time.Now().Add(5 * time.Second))
	m := &Manager{client: afc.NewFromConn(host), connection: host, serial: "iphone", scope: "media"}
	done := make(chan error, 1)
	t.Cleanup(func() {
		_ = host.Close()
		_ = device.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	go func() {
		handles := map[uint64]*afcTestHandle{}
		var handleID uint64
		for {
			header := make([]byte, 40)
			if _, err := io.ReadFull(device, header); err != nil {
				done <- nil
				return
			}
			entire, this := binary.LittleEndian.Uint64(header[8:]), binary.LittleEndian.Uint64(header[16:])
			if entire < 40 || entire > 2*1024*1024 || this > entire {
				done <- fmt.Errorf("bad AFC header")
				return
			}
			payload := make([]byte, entire-40)
			if _, err := io.ReadFull(device, payload); err != nil {
				done <- err
				return
			}
			op := binary.LittleEndian.Uint64(header[32:])
			name := strings.TrimRight(string(payload[:this-40]), "\x00")
			var body []byte
			var status uint64
			responseOp := uint64(1)
			switch op {
			case 0xA:
				n, ok := nodes[name]
				if !ok {
					status = 8
					break
				}
				responseOp = 2
				body = []byte("st_ifmt\x00" + n.kind + "\x00st_size\x00" + strconv.Itoa(len(n.data)) + "\x00st_linktarget\x00" + n.data + "\x00")
			case 3:
				if n, ok := nodes[name]; !ok || n.kind != "S_IFDIR" {
					status = 8
					break
				}
				var names []string
				for p := range nodes {
					if p != name && path.Dir(p) == name {
						names = append(names, path.Base(p))
					}
				}
				sort.Strings(names)
				responseOp = 2
				body = []byte(strings.Join(names, "\x00") + "\x00")
			case 9:
				if _, ok := nodes[name]; ok {
					status = 16
					break
				}
				nodes[name] = afcTestNode{kind: "S_IFDIR"}
			case 0xD:
				mode := binary.LittleEndian.Uint64(payload)
				name = strings.TrimRight(string(payload[8:]), "\x00")
				if mode == 1 {
					if _, ok := nodes[name]; !ok {
						status = 8
						break
					}
				} else {
					nodes[name] = afcTestNode{kind: "S_IFREG"}
				}
				handleID++
				handles[handleID] = &afcTestHandle{path: name}
				body = make([]byte, 8)
				binary.LittleEndian.PutUint64(body, handleID)
				responseOp = 0xE
			case 0xF:
				h := handles[binary.LittleEndian.Uint64(payload)]
				data := nodes[h.path].data
				end := min(len(data), h.offset+int(binary.LittleEndian.Uint64(payload[8:])))
				body = []byte(data[h.offset:end])
				h.offset = end
				responseOp = 2
			case 0x10:
				if failWrite {
					status = 10
					break
				}
				h := handles[binary.LittleEndian.Uint64(payload)]
				n := nodes[h.path]
				n.data += string(payload[this-40:])
				nodes[h.path] = n
			case 0x14:
				delete(handles, binary.LittleEndian.Uint64(payload))
			case 0x1C:
				if binary.LittleEndian.Uint64(payload) != 2 {
					done <- fmt.Errorf("link was not symbolic")
					return
				}
				parts := strings.Split(string(payload[8:]), "\x00")
				nodes[parts[1]] = afcTestNode{kind: "S_IFLNK", data: parts[0]}
			case 0x18:
				parts := strings.Split(string(payload), "\x00")
				src, dst := parts[0], parts[1]
				if _, ok := nodes[dst]; ok {
					status = 16
					break
				}
				moved := map[string]afcTestNode{}
				for p, n := range nodes {
					if p == src || strings.HasPrefix(p, src+"/") {
						moved[dst+strings.TrimPrefix(p, src)] = n
						delete(nodes, p)
					}
				}
				for p, n := range moved {
					nodes[p] = n
				}
			case 8, 0x22:
				delete(nodes, name)
				if op == 0x22 {
					for p := range nodes {
						if strings.HasPrefix(p, name+"/") {
							delete(nodes, p)
						}
					}
				}
			default:
				done <- fmt.Errorf("unexpected AFC opcode %x", op)
				return
			}
			if responseOp == 1 {
				body = make([]byte, 8)
				binary.LittleEndian.PutUint64(body, status)
			}
			binary.LittleEndian.PutUint64(header[8:], uint64(40+len(body)))
			binary.LittleEndian.PutUint64(header[16:], 40)
			if responseOp == 1 || responseOp == 0xE {
				binary.LittleEndian.PutUint64(header[16:], uint64(40+len(body)))
			}
			binary.LittleEndian.PutUint64(header[32:], responseOp)
			if _, err := device.Write(append(header, body...)); err != nil {
				done <- nil
				return
			}
		}
	}()
	return m, nodes
}

func TestIOSPasteCopiesTreeAndMovesWithoutOverwrite(t *testing.T) {
	m, nodes := pasteFixture(t, false)
	ctx := context.Background()
	if err := m.Paste(ctx, "iphone", "media", "/Documents/source", "/Target", false); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/nested", "/nested/file", "/empty", "/link", "/dangling"} {
		if a, b := nodes["/Documents/source"+suffix], nodes["/Target/source"+suffix]; a != b {
			t.Fatalf("copy mismatch %s: %+v vs %+v", suffix, a, b)
		}
	}
	if err := m.Paste(ctx, "iphone", "media", "/Documents/source", "/Moved", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := nodes["/Documents/source"]; ok {
		t.Fatal("move retained source")
	}
	if n := nodes["/Moved/source/nested/file"]; n.data != "binary\x00\xffdata" {
		t.Fatalf("move lost contents: %+v", n)
	}
	for p := range nodes {
		if strings.Contains(p, ".goscrcpy-paste-") {
			t.Fatal("staging leaked", p)
		}
	}
	if nodes["/Documents/keep"].data != "preserve" {
		t.Fatal("modified symlink target")
	}
}

func TestIOSPasteRejectsConflictsAndDirectoryAliases(t *testing.T) {
	for _, tc := range []struct {
		name, parent string
		move         bool
	}{{"copy-conflict", "/Target", false}, {"move-conflict", "/Target", true}, {"alias-subtree", "/Alias", false}} {
		t.Run(tc.name, func(t *testing.T) {
			m, nodes := pasteFixture(t, false)
			nodes["/Target/source"] = afcTestNode{kind: "S_IFREG", data: "existing"}
			if err := m.Paste(context.Background(), "iphone", "media", "/Documents/source", tc.parent, tc.move); err == nil {
				t.Fatal("unsafe paste accepted")
			}
			if nodes["/Target/source"].data != "existing" {
				t.Fatal("existing target changed")
			}
			if nodes["/Documents/source/nested/file"].data != "binary\x00\xffdata" {
				t.Fatal("source changed")
			}
		})
	}
}

func TestIOSCopyFailurePreservesSourceAndCleansStaging(t *testing.T) {
	m, nodes := pasteFixture(t, true)
	if err := m.Paste(context.Background(), "iphone", "media", "/Documents/source", "/Target", false); err == nil {
		t.Fatal("failed write reported as copied")
	}
	if _, ok := nodes["/Target/source"]; ok {
		t.Fatal("published incomplete copy")
	}
	if nodes["/Documents/source/nested/file"].data != "binary\x00\xffdata" {
		t.Fatal("source changed")
	}
	for p := range nodes {
		if strings.Contains(p, ".goscrcpy-paste-") {
			t.Fatal("staging leaked", p)
		}
	}
}

func TestPasteRejectsRootAmbiguousAndSelfTargets(t *testing.T) {
	for _, p := range [][2]string{{"/", "/Target"}, {"relative", "/Target"}, {"/Documents/source", "/Documents/source"}, {"/Documents/source", "/Documents/source/nested"}, {"/Documents/source", "/Documents"}, {"/Documents/source", "/Target/.."}} {
		if _, err := PasteTarget(p[0], p[1]); err == nil {
			t.Fatal("unsafe paste accepted", p)
		}
	}
}
