package tcp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// responseFrame builds a plaintext 7709 response frame (16-byte header +
// payload) with the given msg_id, msg_type and body. zip_length == length so
// the payload is returned verbatim by frame.DecodeResponseBytes.
func responseFrame(msgID uint32, msgType uint16, body []byte) []byte {
	buf := make([]byte, 16+len(body))
	copy(buf[0:4], []byte{0xB1, 0xCB, 0x74, 0x00})
	buf[4] = 0x00 // control
	binary.LittleEndian.PutUint32(buf[5:9], msgID)
	buf[9] = 0x00 // reserved
	binary.LittleEndian.PutUint16(buf[10:12], msgType)
	binary.LittleEndian.PutUint16(buf[12:14], uint16(len(body))) // zipLen
	binary.LittleEndian.PutUint16(buf[14:16], uint16(len(body))) // rawLen
	copy(buf[16:], body)
	return buf
}

// readRequestFrame parses a 12-byte-header request frame from r.
func readRequestFrame(r io.Reader) (msgID uint32, msgType uint16, data []byte, err error) {
	var hdr [12]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	msgID = binary.LittleEndian.Uint32(hdr[1:5])
	msgType = binary.LittleEndian.Uint16(hdr[10:12])
	n := int(binary.LittleEndian.Uint16(hdr[6:8])) // len(data)+2
	dataLen := n - 2
	data = make([]byte, dataLen)
	if _, err = io.ReadFull(r, data); err != nil {
		return 0, 0, nil, err
	}
	return msgID, msgType, data, nil
}

// mockServer is a scripted TCP server for pool tests.
type mockServer struct {
	t       *testing.T
	ln      net.Listener
	addr    string
	handler func(net.Conn)
}

func newMockServer(t *testing.T, handler func(net.Conn)) *mockServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &mockServer{t: t, ln: ln, addr: ln.Addr().String(), handler: handler}
	go s.serve()
	t.Cleanup(s.close)
	return s
}

func (s *mockServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handler(c)
	}
}

func (s *mockServer) close() { s.ln.Close() }

func TestExecHappyPath(t *testing.T) {
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		msgID, msgType, data, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		body := append([]byte("pong:"), data...)
		c.Write(responseFrame(msgID, msgType, body))
	})

	p := NewPool([]string{s.addr}, 1)
	defer p.Close()

	got, err := p.Exec(0x044E, []byte("ping"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if want := "pong:ping"; string(got) != want {
		t.Fatalf("Exec body = %q, want %q", got, want)
	}
}

func TestExecRoutedEncodesRouteChannel(t *testing.T) {
	const route, channel = 0x7E, 0x2D
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		msgID, msgType, _, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		// Server asserts the high bytes carry route/channel.
		if got := byte(msgID >> 16); got != route {
			t.Errorf("route byte = %#x, want %#x", got, route)
		}
		if got := byte(msgID >> 8); got != channel {
			t.Errorf("channel byte = %#x, want %#x", got, channel)
		}
		c.Write(responseFrame(msgID, msgType, []byte("routed")))
	})

	p := NewPool([]string{s.addr}, 1)
	defer p.Close()

	got, err := p.ExecRouted(0x044E, route, channel, nil)
	if err != nil {
		t.Fatalf("ExecRouted: %v", err)
	}
	if string(got) != "routed" {
		t.Fatalf("ExecRouted body = %q, want %q", got, "routed")
	}
}

func TestExecSkipsStaleFrame(t *testing.T) {
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		msgID, msgType, _, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		// A stale frame with a different msg_id arrives first.
		c.Write(responseFrame(msgID+1, msgType, []byte("stale")))
		// Then the matching frame.
		c.Write(responseFrame(msgID, msgType, []byte("fresh")))
	})

	p := NewPool([]string{s.addr}, 1)
	defer p.Close()

	got, err := p.Exec(0x044E, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(got) != "fresh" {
		t.Fatalf("Exec body = %q, want %q (stale frame must be skipped)", got, "fresh")
	}
}

func TestExecTimeoutOnMissingMatch(t *testing.T) {
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		msgID, msgType, _, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		// Send a stale frame, then go silent: the matching frame never comes.
		c.Write(responseFrame(msgID+1, msgType, []byte("stale")))
		time.Sleep(2 * time.Second) // keep the conn open past the pool timeout
	})

	p := NewPool([]string{s.addr}, 1)
	defer p.Close()
	p.Timeout = 100 * time.Millisecond

	_, err := p.Exec(0x044E, nil)
	if err == nil {
		t.Fatal("Exec returned nil error, want timeout")
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("Exec error = %v, want a timeout (net.Error with Timeout()==true)", err)
	}
}

func TestSetupRunsLoginBeforeExec(t *testing.T) {
	const (
		msgLogin1 uint16 = 0x000d
		msgLogin2 uint16 = 0x0fdb
	)
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		// LOGIN1 must arrive first.
		msgID, msgType, _, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read login1: %v", err)
			return
		}
		if msgType != msgLogin1 {
			t.Errorf("first frame msg_type = %#x, want LOGIN1 %#x", msgType, msgLogin1)
		}
		c.Write(responseFrame(msgID, msgType, []byte("login1-ok")))

		// LOGIN2 must arrive second, with the 31-byte magic payload.
		msgID, msgType, data, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read login2: %v", err)
			return
		}
		if msgType != msgLogin2 {
			t.Errorf("second frame msg_type = %#x, want LOGIN2 %#x", msgType, msgLogin2)
		}
		if len(data) != 31 {
			t.Errorf("login2 payload len = %d, want 31", len(data))
		}
		c.Write(responseFrame(msgID, msgType, []byte("login2-ok")))

		// The real request must follow the two logins.
		msgID, msgType, data, err = readRequestFrame(br)
		if err != nil {
			t.Errorf("read real request: %v", err)
			return
		}
		c.Write(responseFrame(msgID, msgType, append([]byte("pong:"), data...)))
	})

	p := NewPool([]string{s.addr}, 1)
	defer p.Close()
	p.Setup = func(exec func(uint16, []byte) ([]byte, error)) error {
		if _, err := exec(msgLogin1, []byte{0x01}); err != nil {
			return err
		}
		if _, err := exec(msgLogin2, make([]byte, 31)); err != nil {
			return err
		}
		return nil
	}

	got, err := p.Exec(0x044E, []byte("ping"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if want := "pong:ping"; string(got) != want {
		t.Fatalf("Exec body = %q, want %q", got, want)
	}
}

func TestPoolFailoverOnConnect(t *testing.T) {
	s := newMockServer(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		msgID, msgType, _, err := readRequestFrame(br)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		c.Write(responseFrame(msgID, msgType, []byte("ok")))
	})

	// First addr is a closed port; the pool must fall over to the live one.
	p := NewPool([]string{"127.0.0.1:1", s.addr}, 1)
	defer p.Close()

	got, err := p.Exec(0x044E, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("Exec body = %q, want %q", got, "ok")
	}
}
