// Package tcp implements a connection pool over 7709 market-data servers with
// synchronous request/response and msg-id correlation.
package tcp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go2tdx/internal/frame"
)

// defaultTimeout bounds a single Exec (write + response read) when no matching
// response frame arrives.
const defaultTimeout = 5 * time.Second

// respPrefix is the 4-byte prefix that begins every 7709 response frame.
var respPrefix = []byte{0xB1, 0xCB, 0x74, 0x00}

// Pool is a fixed-size pool of connections. Exec calls are round-robined
// across the pool's connections; a connection that errors is re-dialed (with
// failover across addrs) on its next use.
type Pool struct {
	addrs []string

	conns []*conn
	rr    atomic.Uint64 // round-robin cursor across conns
	dialc atomic.Uint64 // rotating cursor across addrs for dial failover

	// Timeout bounds a single Exec. Zero means defaultTimeout.
	Timeout time.Duration

	// Setup, if non-nil, runs on each newly-established connection (holding that
	// connection's mutex) before it serves any Exec. The exec closure it receives
	// performs one synchronous request/response on that same connection without
	// re-acquiring the lock.
	Setup func(exec func(msgType uint16, data []byte) ([]byte, error)) error
}

type conn struct {
	nc net.Conn
	br *bufio.Reader
	mu sync.Mutex
	id uint32 // monotonic msg-id counter
}

// NewPool creates a pool of size connections. addrs are tried in rotation on
// each dial, so a connect failure falls over to the next address.
func NewPool(addrs []string, size int) *Pool {
	if size < 1 {
		size = 1
	}
	p := &Pool{addrs: append([]string(nil), addrs...)}
	p.conns = make([]*conn, size)
	for i := range p.conns {
		p.conns[i] = &conn{}
	}
	return p
}

func (p *Pool) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultTimeout
}

// Exec sends one request and returns the body of the matching response.
func (p *Pool) Exec(msgType uint16, data []byte) ([]byte, error) {
	return p.exec(p.getConn(), msgType, data, nil)
}

// ExecRouted sends a request whose on-wire msg_id encodes route and channel in
// its high bytes: (id & 0xFF) | (channel << 8) | (route << 16).
func (p *Pool) ExecRouted(msgType uint16, route, channel byte, data []byte) ([]byte, error) {
	return p.exec(p.getConn(), msgType, data, &routeInfo{route: route, channel: channel})
}

type routeInfo struct{ route, channel byte }

func (p *Pool) getConn() *conn {
	i := p.rr.Add(1) - 1
	return p.conns[int(i%uint64(len(p.conns)))]
}

// Close closes every connection in the pool.
func (p *Pool) Close() error {
	var firstErr error
	for _, c := range p.conns {
		c.mu.Lock()
		if c.nc != nil {
			if err := c.nc.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			c.nc = nil
			c.br = nil
		}
		c.mu.Unlock()
	}
	return firstErr
}

func (p *Pool) exec(c *conn, msgType uint16, data []byte, rt *routeInfo) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensure(p); err != nil {
		return nil, err
	}

	body, err := c.doExec(p, msgType, data, rt)
	if err != nil && isStaleConnErr(err) {
		// 闲置连接被服务器断开（broken pipe/EOF/reset）：doExec 已 kill 连接，重拨重试一次，对调用者透明。
		if e := c.ensure(p); e == nil {
			body, err = c.doExec(p, msgType, data, rt)
		}
	}
	return body, err
}

// isStaleConnErr 判断错误是否为「连接被对端断开」类错误（broken pipe/EOF/reset/closed）。
// 这类错误重拨后重试即可成功，无需向上抛（超时/解码错误不在内）。
func isStaleConnErr(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET)
}

// doExec performs one synchronous request/response on c. It does NOT lock:
// callers must hold c.mu (or be running inside Setup, where ensure already
// holds it).
func (c *conn) doExec(p *Pool, msgType uint16, data []byte, rt *routeInfo) ([]byte, error) {
	c.id++
	wireID := uint32(c.id)
	if rt != nil {
		wireID = uint32(c.id&0xFF) | uint32(rt.channel)<<8 | uint32(rt.route)<<16
	}

	if err := c.nc.SetDeadline(time.Now().Add(p.timeout())); err != nil {
		return nil, err
	}
	defer c.nc.SetDeadline(time.Time{})

	req := frame.EncodeRequest(wireID, msgType, data)
	if _, err := c.nc.Write(req); err != nil {
		c.kill()
		return nil, fmt.Errorf("tcp: write request: %w", err)
	}

	for {
		resp, err := readResponseFrame(c.br)
		if err != nil {
			c.kill()
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return nil, fmt.Errorf("tcp: timed out waiting for response msg_id %d: %w", wireID, err)
			}
			return nil, fmt.Errorf("tcp: read response: %w", err)
		}
		if resp.MsgID == wireID {
			// Clone: the plaintext path aliases the frame buffer we built.
			return bytes.Clone(resp.Body), nil
		}
		// Non-matching frame (out-of-order response or push): skip it.
	}
}

// ensure dials a connection if none is open, then runs the Setup hook (if any)
// on a newly-established connection before it serves any Exec.
func (c *conn) ensure(p *Pool) error {
	if c.nc != nil {
		return nil
	}
	nc, err := p.dial()
	if err != nil {
		return err
	}
	c.nc = nc
	c.br = bufio.NewReaderSize(nc, 64*1024)
	if p.Setup != nil {
		if err := p.Setup(func(msgType uint16, data []byte) ([]byte, error) {
			return c.doExec(p, msgType, data, nil)
		}); err != nil {
			c.kill()
			return err
		}
	}
	return nil
}

// kill closes and clears the connection so the next Exec re-dials.
func (c *conn) kill() {
	if c.nc != nil {
		c.nc.Close()
		c.nc = nil
		c.br = nil
	}
}

// dial tries each address in rotation until one connects.
func (p *Pool) dial() (net.Conn, error) {
	n := len(p.addrs)
	if n == 0 {
		return nil, errors.New("tcp: no addresses configured")
	}
	start := int(p.dialc.Add(1)-1) % n
	var lastErr error
	for i := 0; i < n; i++ {
		addr := p.addrs[(start+i)%n]
		nc, err := net.DialTimeout("tcp", addr, p.timeout())
		if err == nil {
			return nc, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("tcp: connect to all %d address(es) failed: %w", n, lastErr)
}

// readResponseFrame reads one full response frame from br, resynchronizing on
// the 4-byte prefix and accumulating across partial reads.
func readResponseFrame(br *bufio.Reader) (*frame.Response, error) {
	var hdr [frame.ResponseHeaderSize]byte
	if err := syncPrefix(br, hdr[:4]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(br, hdr[4:]); err != nil {
		return nil, err
	}
	zipLen := int(binary.LittleEndian.Uint16(hdr[12:14]))
	payload := make([]byte, zipLen)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, err
	}
	full := make([]byte, 0, frame.ResponseHeaderSize+zipLen)
	full = append(full, hdr[:]...)
	full = append(full, payload...)
	return frame.DecodeResponseBytes(full)
}

// syncPrefix discards bytes until the 4-byte response prefix is next in the
// reader, then copies it into dst.
func syncPrefix(br *bufio.Reader, dst []byte) error {
	for {
		b, err := br.Peek(4)
		if err != nil {
			return err
		}
		if bytes.Equal(b, respPrefix) {
			copy(dst, b)
			_, err := br.Discard(4)
			return err
		}
		if _, err := br.Discard(1); err != nil {
			return err
		}
	}
}
