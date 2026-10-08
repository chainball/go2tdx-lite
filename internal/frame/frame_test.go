package frame

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// fixture reads a frozen 7709 fixture capture.
func fixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", "testdata", "7709"}, parts...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return b
}

func TestEncodeRequestFrozenExample(t *testing.T) {
	// frame.rs frozen example: msg_id=123, type=0x044E, data=00 00 a7 26 35 01
	got := EncodeRequest(123, 0x044E, []byte{0x00, 0x00, 0xA7, 0x26, 0x35, 0x01})
	want, err := hex.DecodeString("0c7b00000001080008004e040000a7263501")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRequest = % x, want % x", got, want)
	}
	if len(got) != RequestHeaderSize+6 {
		t.Fatalf("len = %d, want %d", len(got), RequestHeaderSize+6)
	}
}

func TestEncodeRequestHeartbeatShape(t *testing.T) {
	// heartbeat fixtures carry an empty-data request; verify the header layout
	// (0x0C | msg_id | 0x01 | len | len | msg_type) against request.bin.
	got := EncodeRequest(805306369, 0x0004, nil)
	want := fixture(t, "heartbeat", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRequest = % x, want % x", got, want)
	}
}

func TestDecodeResponseNormal(t *testing.T) {
	r, err := DecodeResponseBytes(fixture(t, "heartbeat", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if r.MsgType != 4 {
		t.Errorf("MsgType = %d, want 4", r.MsgType)
	}
	// 0x30000001, per the fixture's metadata.json message_id.
	if r.MsgID != 805306369 {
		t.Errorf("MsgID = %d, want 805306369", r.MsgID)
	}
	if len(r.Body) == 0 {
		t.Fatal("Body is empty, want non-empty")
	}
	if want := "000000000000a8263501"; hex.EncodeToString(r.Body) != want {
		t.Errorf("Body = %x, want %s", r.Body, want)
	}
}

func TestDecodeResponseCompressed(t *testing.T) {
	// zip_length != length: payload is zlib and must be inflated to `length` bytes.
	raw := fixture(t, "heartbeat", "compressed", "response.bin")
	if zipLen := int(raw[12]) | int(raw[13])<<8; zipLen == int(raw[14])|int(raw[15])<<8 {
		t.Fatalf("fixture is not compressed: zip_length == length == %d", zipLen)
	}
	r, err := DecodeResponseBytes(raw)
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if r.MsgType != 4 {
		t.Errorf("MsgType = %d, want 4", r.MsgType)
	}
	if r.MsgID != 822083585 { // fixture metadata.json message_id
		t.Errorf("MsgID = %d, want 822083585", r.MsgID)
	}
	if len(r.Body) == 0 {
		t.Fatal("Body is empty, want non-empty")
	}
	// Same logical heartbeat payload as the plaintext normal fixture.
	plain, err := DecodeResponseBytes(fixture(t, "heartbeat", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("decode normal fixture: %v", err)
	}
	if !bytes.Equal(r.Body, plain.Body) {
		t.Errorf("inflated Body = %x, want %x", r.Body, plain.Body)
	}
}

func TestDecodeResponseBadCompression(t *testing.T) {
	// Truncated zlib stream (78 9c 00) declared as compressed: must error.
	if _, err := DecodeResponseBytes(fixture(t, "heartbeat", "bad_compression", "response.bin")); err == nil {
		t.Fatal("DecodeResponseBytes returned nil error, want error")
	}
}

func TestDecodeResponseResyncsOnPrefix(t *testing.T) {
	raw := fixture(t, "heartbeat", "normal", "response.bin")
	junk := append([]byte{0x0A, 0xFF, 0x91, 0xB1, 0xCB}, raw...) // 0xB1 0xCB 0x74 0x00 must be re-found
	r, err := DecodeResponseBytes(junk)
	if err != nil {
		t.Fatalf("DecodeResponseBytes with junk prefix: %v", err)
	}
	if r.MsgType != 4 || len(r.Body) == 0 {
		t.Fatalf("MsgType = %d, Body len = %d; want 4 and non-empty", r.MsgType, len(r.Body))
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	if _, err := DecodeResponseBytes([]byte{0x00, 0x01, 0x02, 0x03}); err == nil {
		t.Error("no prefix: want error, got nil")
	}
	if _, err := DecodeResponseBytes([]byte{0xB1, 0xCB, 0x74, 0x00, 0x00, 0x01}); err == nil {
		t.Error("short header: want error, got nil")
	}
	short := fixture(t, "heartbeat", "compressed", "response.bin")[:ResponseHeaderSize+5]
	if _, err := DecodeResponseBytes(short); err == nil {
		t.Error("short payload: want error, got nil")
	}
}
