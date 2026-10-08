package go2tdx

import (
	"errors"
	"fmt"
	"time"

	"github.com/chainball/go2tdx-lite/internal/tcp"
)

// errShortBody reports a response payload too small to decode.
var errShortBody = errors.New("go2tdx: short response body")

// Client is a 7709 market-data client.
type Client struct {
	pool *tcp.Pool // 主站
}

// Dial builds a Client over the given servers. The tcp pool dials lazily,
// so this never fails;
// the error return is kept for signature stability.
// loginSetup performs the per-connection 7709 setup: LOGIN1 (0x000D) is
// required before a connection serves commands. LOGIN2 (0x0FDB) is NOT sent —
// the classic 7709 servers (probed against gotdx/eltdx server lists) do not
// respond to it.
func loginSetup(exec func(uint16, []byte) ([]byte, error)) error {
	_, err := exec(msgHandshake, []byte{0x01})
	return err
}

// Dial dials a single server (test/debug use; no round-robin across servers).
// The tcp pool dials lazily, so this never fails; the error return is kept
// for signature stability.
func Dial(server Server) (*Client, error) {
	return dial([]Server{server})
}

// dial builds a Client over the given servers.
func dial(servers []Server) (*Client, error) {
	pool := tcp.NewPool(serverAddrs(servers), 4)
	pool.Setup = loginSetup
	return &Client{pool: pool}, nil
}

// DialBest probes StandardServers and dials the n fastest reachable servers.
// n <= 0 defaults to 32.
func DialBest(n int) (*Client, error) {
	servers := BestServers(n)
	if len(servers) == 0 {
		return nil, errors.New("go2tdx: no reachable servers")
	}
	return dial(servers)
}

// Close closes both pools. It is safe to call on a zero-value Client.
func (c *Client) Close() error {
	var firstErr error
	if c.pool != nil {
		if err := c.pool.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func serverAddrs(servers []Server) []string {
	addrs := make([]string, len(servers))
	for i, s := range servers {
		addrs[i] = s.addr()
	}
	return addrs
}

// SessionAPI exposes session commands (handshake, heartbeat).
type SessionAPI struct{ c *Client }

// CodesAPI exposes security-list commands.
type CodesAPI struct{ c *Client }

// BarsAPI exposes kline commands.
type BarsAPI struct{ c *Client }

// CorporateAPI exposes corporate-action and finance commands.
type CorporateAPI struct{ c *Client }

// MinutesAPI exposes intraday minute-bar commands.
type MinutesAPI struct{ c *Client }

// TradesAPI exposes tick/trade commands.
type TradesAPI struct{ c *Client }

// AuctionsAPI exposes auction commands.
type AuctionsAPI struct{ c *Client }

// QuotesAPI exposes quote/snapshot commands.
type QuotesAPI struct{ c *Client }

// ResourcesAPI exposes file-content commands.
type ResourcesAPI struct{ c *Client }

func (c *Client) Session() *SessionAPI     { return &SessionAPI{c} }
func (c *Client) Codes() *CodesAPI         { return &CodesAPI{c} }
func (c *Client) Bars() *BarsAPI           { return &BarsAPI{c} }
func (c *Client) Corporate() *CorporateAPI { return &CorporateAPI{c} }
func (c *Client) Minutes() *MinutesAPI     { return &MinutesAPI{c} }
func (c *Client) Trades() *TradesAPI       { return &TradesAPI{c} }
func (c *Client) Auctions() *AuctionsAPI   { return &AuctionsAPI{c} }
func (c *Client) Quotes() *QuotesAPI       { return &QuotesAPI{c} }
func (c *Client) Resources() *ResourcesAPI { return &ResourcesAPI{c} }

// trimNUL cuts b at the first 0x00 byte.
func trimNUL(b []byte) []byte {
	for i, v := range b {
		if v == 0 {
			return b[:i]
		}
	}
	return b
}

// padDate formats a yyyymmdd u32 as "%04d%02d%02d".
func padDate(v uint32) string {
	year := v / 10000
	month := (v / 100) % 100
	day := v % 100
	return fmt.Sprintf("%04d%02d%02d", year, month, day)
}

// todayYMD returns today's date as a yyyymmdd u32 (local time).
func todayYMD() uint32 {
	now := time.Now()
	return uint32(now.Year()*10000 + int(now.Month())*100 + now.Day())
}
