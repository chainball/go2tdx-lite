package go2tdx

import (
	"encoding/binary"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"go2tdx/internal/frame"
)

// probeResult is one server's speed-test outcome.
type probeResult struct {
	server Server
	ms     int64 // LOGIN1 round-trip latency in milliseconds; -1 if failed
}

// BestServers probes every server in StandardServers concurrently with a
// LOGIN1 handshake round-trip, keeps the reachable ones, and returns the n
// fastest in ascending latency order. n <= 0 defaults to 32.
//
// If fewer than n pass, it retries once with a longer read timeout (3s vs the
// initial 1.5s) to let slow servers respond. If still fewer than n, it returns
// however many are reachable (the pool shrinks accordingly); an empty result
// means no server passed the probe.
func BestServers(n int) []Server {
	if n <= 0 {
		n = 32
	}
	servers := probeBest(1500 * time.Millisecond)
	if len(servers) < n {
		servers = probeBest(3 * time.Second)
	}
	if len(servers) > n {
		servers = servers[:n]
	}
	return servers
}

// probeBest probes every StandardServer concurrently and returns the reachable
// ones sorted by latency (ascending).
func probeBest(readTimeout time.Duration) []Server {
	results := make([]probeResult, len(StandardServers))
	var wg sync.WaitGroup
	for i, s := range StandardServers {
		wg.Add(1)
		go func(i int, s Server) {
			defer wg.Done()
			results[i] = probeResult{s, probeLatency(s, readTimeout)}
		}(i, s)
	}
	wg.Wait()

	var ok []probeResult
	for _, r := range results {
		if r.ms >= 0 {
			ok = append(ok, r)
		}
	}
	sort.Slice(ok, func(i, j int) bool { return ok[i].ms < ok[j].ms })
	out := make([]Server, len(ok))
	for i, r := range ok {
		out[i] = r.server
	}
	return out
}

// probeLatency dials s, performs a LOGIN1 handshake, and returns the
// round-trip latency in milliseconds, or -1 on any failure or timeout.
// The dial timeout is fixed at 1.5s; readTimeout bounds the response read.
func probeLatency(s Server, readTimeout time.Duration) int64 {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", s.addr(), 1500*time.Millisecond)
	if err != nil {
		return -1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(readTimeout))
	req := frame.EncodeRequest(1, msgHandshake, []byte{0x01})
	if _, err := conn.Write(req); err != nil {
		return -1
	}
	hdr := make([]byte, frame.ResponseHeaderSize)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return -1
	}
	zipLen := int(binary.LittleEndian.Uint16(hdr[12:14]))
	if _, err := io.ReadFull(conn, make([]byte, zipLen)); err != nil {
		return -1
	}
	return time.Since(start).Milliseconds()
}
