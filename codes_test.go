package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/chainball/go2tdx-lite/internal/frame"
	"github.com/chainball/go2tdx-lite/internal/gbk"
)

// codesMeta reads the top-level command_code / message_id fields of a frozen
// fixture's metadata.json (the nested request_context is left unparsed).
type codesMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readCodesMeta(t *testing.T, parts ...string) codesMeta {
	t.Helper()
	p := filepath.Join(append([]string{"testdata", "7709"}, append(parts, "metadata.json")...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m codesMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// --- encode ---

func TestEncodeSecurityListRequest(t *testing.T) {
	cases := []struct {
		sub    string
		market uint16
		start  uint32
		limit  uint32
	}{
		{"bj_empty", 2, 0, 1600}, // bj
		{"normal", 0, 0, 1600},   // sz
		{"sh_empty", 1, 0, 1600}, // sh
	}
	for _, tc := range cases {
		meta := readCodesMeta(t, "security_list", tc.sub)
		if meta.CommandCode != 1101 {
			t.Errorf("security_list/%s command_code = %d, want 1101", tc.sub, meta.CommandCode)
		}

		// Request data: market u16 | start u32 | limit u32 | 4x0.
		data := make([]byte, 14)
		binary.LittleEndian.PutUint16(data[0:2], tc.market)
		binary.LittleEndian.PutUint32(data[2:6], tc.start)
		binary.LittleEndian.PutUint32(data[6:10], tc.limit)

		got := frame.EncodeRequest(meta.MessageID, 1101, data)
		want := sessionFixture(t, "security_list", tc.sub, "request.bin")
		if !bytes.Equal(got, want) {
			t.Errorf("security_list/%s request = % x, want % x", tc.sub, got, want)
		}
	}
}

func TestEncodeSecurityCountRequest(t *testing.T) {
	meta := readCodesMeta(t, "security_count", "normal")
	if meta.CommandCode != 1102 {
		t.Errorf("security_count/normal command_code = %d, want 1102", meta.CommandCode)
	}

	// Request data: market u16 | client_date u32 (yyyymmdd).
	data := make([]byte, 6)
	binary.LittleEndian.PutUint16(data[0:2], 0) // sz
	binary.LittleEndian.PutUint32(data[2:6], 20260519)

	got := frame.EncodeRequest(meta.MessageID, 1102, data)
	want := sessionFixture(t, "security_count", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("security_count/normal request = % x, want % x", got, want)
	}
}

// --- decode: security_list ---

func TestDecodeSecurityList(t *testing.T) {
	// Synthetic 37B record: code 6B | multiple u16 | name 16B GBK NUL-pad |
	// volume_ratio_base f32 | decimal u8 | previous_close f32 | unknown3 4B.
	rec := make([]byte, 37)
	copy(rec[0:6], "000001")
	binary.LittleEndian.PutUint16(rec[6:8], 100) // multiple
	name, err := gbk.Encode("平安银行")
	if err != nil {
		t.Fatalf("gbk.Encode: %v", err)
	}
	copy(rec[8:24], name) // rest stays NUL-padded
	rec[28] = 2           // decimal
	binary.LittleEndian.PutUint32(rec[29:33], math.Float32bits(10.99))

	body := make([]byte, 2+37)
	binary.LittleEndian.PutUint16(body[0:2], 1) // count
	copy(body[2:], rec)

	got, err := decodeSecurityList(body)
	if err != nil {
		t.Fatalf("decodeSecurityList: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decodeSecurityList len = %d, want 1 (%+v)", len(got), got)
	}
	s := got[0]
	if s.Code != "000001" || s.Name != "平安银行" || s.Multiple != 100 || s.Decimal != 2 {
		t.Errorf("decodeSecurityList = %+v, want {Code:000001 Name:平安银行 Multiple:100 Decimal:2}", s)
	}
	// previous_close is stored as f32, so the f64 widening is 10.989999771118164.
	if want := float64(math.Float32frombits(math.Float32bits(10.99))); s.PreClose != want {
		t.Errorf("PreClose = %v, want %v", s.PreClose, want)
	}
}

func TestDecodeSecurityListFixture(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "security_list", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if resp.MsgType != 1101 {
		t.Errorf("msg_type = %d, want 1101", resp.MsgType)
	}
	got, err := decodeSecurityList(resp.Body)
	if err != nil {
		t.Fatalf("decodeSecurityList: %v", err)
	}
	want := Security{
		Code:     "000001",
		Name:     "平安银行",
		Multiple: 100,
		Decimal:  2,
		PreClose: float64(math.Float32frombits(math.Float32bits(10.99))),
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("decodeSecurityList = %+v, want [%+v]", got, want)
	}
}

func TestDecodeSecurityListEmpty(t *testing.T) {
	// bj_empty / sh_empty responses carry count = 0.
	for _, sub := range []string{"bj_empty", "sh_empty"} {
		resp, err := frame.DecodeResponseBytes(sessionFixture(t, "security_list", sub, "response.bin"))
		if err != nil {
			t.Fatalf("%s: DecodeResponseBytes: %v", sub, err)
		}
		got, err := decodeSecurityList(resp.Body)
		if err != nil {
			t.Fatalf("%s: decodeSecurityList: %v", sub, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: decodeSecurityList = %+v, want empty", sub, got)
		}
	}
}

func TestDecodeSecurityListShortBody(t *testing.T) {
	for _, n := range []int{0, 1} {
		if _, err := decodeSecurityList(make([]byte, n)); err == nil {
			t.Errorf("decodeSecurityList(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

func TestDecodeSecurityListTruncatedRecord(t *testing.T) {
	// count claims 2 records but only one full 37B record is present: the
	// partial tail is dropped, no error.
	body := make([]byte, 2+37)
	binary.LittleEndian.PutUint16(body[0:2], 2)
	copy(body[2:8], "000001")
	got, err := decodeSecurityList(body)
	if err != nil {
		t.Fatalf("decodeSecurityList: %v", err)
	}
	if len(got) != 1 || got[0].Code != "000001" {
		t.Fatalf("decodeSecurityList = %+v, want exactly one 000001 record", got)
	}
}

// --- decode: security_count ---

func TestDecodeSecurityCount(t *testing.T) {
	body := make([]byte, 2)
	binary.LittleEndian.PutUint16(body, 23285)

	got, err := decodeSecurityCount(body)
	if err != nil {
		t.Fatalf("decodeSecurityCount: %v", err)
	}
	if got != 23285 {
		t.Fatalf("decodeSecurityCount = %d, want 23285", got)
	}
}

func TestDecodeSecurityCountFixture(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "security_count", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if resp.MsgType != 1102 {
		t.Errorf("msg_type = %d, want 1102", resp.MsgType)
	}
	got, err := decodeSecurityCount(resp.Body)
	if err != nil {
		t.Fatalf("decodeSecurityCount: %v", err)
	}
	if got != 23285 {
		t.Fatalf("decodeSecurityCount = %d, want 23285", got)
	}
}

func TestDecodeSecurityCountShortBody(t *testing.T) {
	for _, n := range []int{0, 1} {
		if _, err := decodeSecurityCount(make([]byte, n)); err == nil {
			t.Errorf("decodeSecurityCount(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestCodesMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Codes().List(0, 0, 1600); err == nil {
		t.Error("List error = nil, want transport error")
	}
	if _, err := c.Codes().Count(0); err == nil {
		t.Error("Count error = nil, want transport error")
	}
}
