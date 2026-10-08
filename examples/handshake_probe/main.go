// Command handshake_probe probes a set of TDX servers: send LOGIN1 (0x000D),
// dump the raw response bytes; then LOGIN2 (0x0FDB), dump raw response bytes.
// It does NOT resync on any prefix — it shows the wire truth.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"time"
)

var servers = []string{
	// eltdx tdx_server.json (subset)
	"116.205.183.150:7709", "111.230.186.52:7709", "129.204.230.128:7709",
	"159.75.29.111:7709", "43.139.95.83:7709", "124.71.9.153:7709",
	// gotdx StandardServer (subset)
	"110.41.2.72:7709", "47.113.94.204:7709", "8.129.174.169:7709",
	"124.70.176.52:7709", "47.100.236.28:7709", "121.36.54.217:7709",
	// the 8 in mqx.ai
	"103.221.142.66:7709", "115.238.90.165:7709", "110.41.147.114:7709", "116.211.121.102:7709",
}

var login2 = []byte{
	0xd5, 0xd0, 0xc9, 0xcc, 0xd6, 0xa4, 0xa8, 0xaf,
	0x00, 0x00, 0x00, 0x8f, 0xc2, 0x25, 0x40, 0x13,
	0x00, 0x00, 0x00, 0xd5, 0x00, 0xc9, 0xcc, 0xbd,
	0xf0, 0xd7, 0xea, 0x00, 0x00, 0x00, 0x02,
}

func reqFrame(msgID uint32, msgType uint16, data []byte) []byte {
	n := len(data) + 2
	b := make([]byte, 12+len(data))
	b[0] = 0x0C
	binary.LittleEndian.PutUint32(b[1:5], msgID)
	b[5] = 0x01
	binary.LittleEndian.PutUint16(b[6:8], uint16(n))
	binary.LittleEndian.PutUint16(b[8:10], uint16(n))
	binary.LittleEndian.PutUint16(b[10:12], msgType)
	copy(b[12:], data)
	return b
}

func dump(label string, b []byte) {
	if len(b) == 0 {
		fmt.Printf("    %s: (empty)\n", label)
		return
	}
	if len(b) > 40 {
		b = b[:40]
	}
	fmt.Printf("    %s: %s\n", label, hex.EncodeToString(b))
}

func probe(addr string) {
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		fmt.Printf("%-22s dial err %v\n", addr, err)
		return
	}
	defer conn.Close()

	// LOGIN1
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	conn.Write(reqFrame(1, 0x000D, []byte{0x01}))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Printf("%-22s LOGIN1 read err %v\n", addr, err)
		return
	}
	dump(addr+" LOGIN1 resp", buf[:n])

	// LOGIN2
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	conn.Write(reqFrame(2, 0x0FDB, login2))
	n, err = conn.Read(buf)
	if err != nil {
		fmt.Printf("%-22s LOGIN2 read err %v\n", addr, err)
		return
	}
	dump(addr+" LOGIN2 resp", buf[:n])
}

func main() {
	for _, s := range servers {
		probe(s)
	}
}
