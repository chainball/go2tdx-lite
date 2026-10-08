package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chainball/go2tdx-lite/internal/frame"
	"github.com/chainball/go2tdx-lite/internal/gbk"
)

// sessionFixture reads a frozen 7709 fixture capture relative to testdata/7709.
func sessionFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	p := filepath.Join(append([]string{"testdata", "7709"}, parts...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return b
}

// --- encode ---

func TestEncodeHeartbeatRequest(t *testing.T) {
	// heartbeat request data is empty, so length = len(data)+2 = 2.
	want, err := hex.DecodeString("0c7b00000001020002000400")
	if err != nil {
		t.Fatal(err)
	}
	got := frame.EncodeRequest(123, 4, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRequest(123, 4, nil) = % x, want % x", got, want)
	}

	// Against the frozen capture: heartbeat/normal/request.bin, msg_id from metadata.json.
	got = frame.EncodeRequest(805306369, 4, nil)
	if want := sessionFixture(t, "heartbeat", "normal", "request.bin"); !bytes.Equal(got, want) {
		t.Fatalf("heartbeat request = % x, want % x", got, want)
	}

	// The other heartbeat captures differ only in msg_id (822083585/586/587).
	for _, msgID := range []uint32{822083585, 822083586, 822083587} {
		sub := map[uint32]string{822083585: "compressed", 822083586: "bad_compression", 822083587: "stale_message"}[msgID]
		got := frame.EncodeRequest(msgID, 4, nil)
		if want := sessionFixture(t, "heartbeat", sub, "request.bin"); !bytes.Equal(got, want) {
			t.Errorf("heartbeat/%s request = % x, want % x", sub, got, want)
		}
	}
}

func TestEncodeHandshakeRequest(t *testing.T) {
	data := []byte{0x01}
	if len(data) != 1 || data[0] != 0x01 {
		t.Fatalf("handshake request data = % x, want 01", data)
	}
	// msg_id from handshake/normal/metadata.json.
	got := frame.EncodeRequest(805306370, 13, data)
	if want := sessionFixture(t, "handshake", "normal", "request.bin"); !bytes.Equal(got, want) {
		t.Fatalf("handshake request = % x, want % x", got, want)
	}
}

// --- decode: heartbeat ---

func TestDecodeHeartbeat(t *testing.T) {
	body := make([]byte, 10)
	binary.LittleEndian.PutUint32(body[6:10], 20260527)

	got, err := decodeHeartbeat(body)
	if err != nil {
		t.Fatalf("decodeHeartbeat: %v", err)
	}
	if got != "20260527" {
		t.Fatalf("decodeHeartbeat = %q, want %q", got, "20260527")
	}
}

func TestDecodeHeartbeatFixture(t *testing.T) {
	// Frozen capture body is `000000000000a8263501` → server_date u32 LE = 20260520.
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "heartbeat", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if len(resp.Body) != 10 {
		t.Fatalf("body len = %d, want 10", len(resp.Body))
	}
	got, err := decodeHeartbeat(resp.Body)
	if err != nil {
		t.Fatalf("decodeHeartbeat: %v", err)
	}
	if got != "20260520" {
		t.Fatalf("decodeHeartbeat = %q, want %q", got, "20260520")
	}
}

func TestDecodeHeartbeatShortBody(t *testing.T) {
	for _, n := range []int{0, 6, 9} {
		if _, err := decodeHeartbeat(make([]byte, n)); err == nil {
			t.Errorf("decodeHeartbeat(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- decode: handshake ---

func TestDecodeHandshake(t *testing.T) {
	body := make([]byte, 189)
	binary.LittleEndian.PutUint16(body[1:3], 2026) // year
	body[3] = 27                                   // day
	body[4] = 5                                    // month
	body[5] = 30                                   // minute
	body[6] = 10                                   // hour
	body[8] = 0                                    // second

	name, err := gbk.Encode("平安银行")
	if err != nil {
		t.Fatalf("gbk.Encode: %v", err)
	}
	copy(body[68:152], name)
	tag, err := gbk.Encode("产品标签")
	if err != nil {
		t.Fatalf("gbk.Encode: %v", err)
	}
	copy(body[160:189], tag)

	info, err := decodeHandshake(body)
	if err != nil {
		t.Fatalf("decodeHandshake: %v", err)
	}
	if info.ServerName != "平安银行" {
		t.Errorf("ServerName = %q, want %q", info.ServerName, "平安银行")
	}
	if info.ProductTag != "产品标签" {
		t.Errorf("ProductTag = %q, want %q", info.ProductTag, "产品标签")
	}
	want := time.Date(2026, time.May, 27, 10, 30, 0, 0, time.UTC)
	got := info.ServerTime
	if got.Year() != want.Year() || got.Month() != want.Month() || got.Day() != want.Day() ||
		got.Hour() != want.Hour() || got.Minute() != want.Minute() || got.Second() != want.Second() {
		t.Fatalf("ServerTime = %v, want fields of %v", got, want)
	}
}

func TestDecodeHandshakeFixture(t *testing.T) {
	// Frozen capture: 2026-05-27T10:30:00, names are ASCII in the fixture.
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "handshake", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if len(resp.Body) < 189 {
		t.Fatalf("body len = %d, want >= 189", len(resp.Body))
	}
	info, err := decodeHandshake(resp.Body)
	if err != nil {
		t.Fatalf("decodeHandshake: %v", err)
	}
	if info.ServerName != "fixture-7709" {
		t.Errorf("ServerName = %q, want %q", info.ServerName, "fixture-7709")
	}
	if info.ProductTag != "fixture-product" {
		t.Errorf("ProductTag = %q, want %q", info.ProductTag, "fixture-product")
	}
	if got := info.ServerTime; got.Year() != 2026 || got.Month() != time.May || got.Day() != 27 ||
		got.Hour() != 10 || got.Minute() != 30 || got.Second() != 0 {
		t.Errorf("ServerTime = %v, want 2026-05-27T10:30:00", got)
	}
}

func TestDecodeHandshakeShortBody(t *testing.T) {
	for _, n := range []int{0, 10, 188} {
		if _, err := decodeHandshake(make([]byte, n)); err == nil {
			t.Errorf("decodeHandshake(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestSessionMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Session().Heartbeat(); err == nil {
		t.Error("Heartbeat error = nil, want transport error")
	}
	if _, err := c.Session().Handshake(); err == nil {
		t.Error("Handshake error = nil, want transport error")
	}
}
