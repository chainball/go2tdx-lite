package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chainball/go2tdx-lite/internal/codec"
	"github.com/chainball/go2tdx-lite/internal/frame"
)

// barsMeta reads the top-level command_code / message_id fields of a frozen
// klines fixture's metadata.json.
type barsMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readBarsMeta(t *testing.T, sub string) barsMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", "klines", sub, "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m barsMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// --- encode ---

func TestEncodeKlinesRequest(t *testing.T) {
	meta := readBarsMeta(t, "normal")
	if meta.CommandCode != 1325 {
		t.Errorf("klines/normal command_code = %d, want 1325", meta.CommandCode)
	}

	// Classic 0x052D request data (26B): market u16 | code 6B | period.raw u16 |
	// period.param u16 (=1) | start u16 | count u16 | 10 trailing zero bytes.
	// No adjust/anchor fields — the gotdx-era servers reject the eltdx 42B form.
	data := make([]byte, 26)
	binary.LittleEndian.PutUint16(data[0:2], 0)     // market sz
	copy(data[2:8], "300308")                       // code
	binary.LittleEndian.PutUint16(data[8:10], 4)    // period.raw = day
	binary.LittleEndian.PutUint16(data[10:12], 1)   // period.param
	binary.LittleEndian.PutUint16(data[12:14], 0)   // start
	binary.LittleEndian.PutUint16(data[14:16], 420) // count
	// data[16:26] stays zero: the 10 trailing zeros, no adjust/anchor.

	// Expected full frame: 12B header + 26B data; n = len(data)+2 = 28.
	want := make([]byte, 12+len(data))
	want[0] = 0x0C
	binary.LittleEndian.PutUint32(want[1:5], meta.MessageID)
	want[5] = 0x01
	binary.LittleEndian.PutUint16(want[6:8], uint16(len(data)+2))
	binary.LittleEndian.PutUint16(want[8:10], uint16(len(data)+2))
	binary.LittleEndian.PutUint16(want[10:12], 1325)
	copy(want[12:], data)

	got := frame.EncodeRequest(meta.MessageID, 1325, data)
	if !bytes.Equal(got, want) {
		t.Fatalf("klines/normal request = % x, want % x", got, want)
	}
}

// --- decode ---

// buildKlinesBody builds a synthetic klines response body: count=1 + one record
// (time u32 LE + 4 price varints + volume u32 + amount u32 [+ up/down u16]).
func buildKlinesBody(t *testing.T, index bool) []byte {
	t.Helper()
	var rec bytes.Buffer
	binary.Write(&rec, binary.LittleEndian, uint32(20260527)) // yyyymmdd (daily)
	codec.WriteVarint(&rec, 10100)                            // open_delta
	codec.WriteVarint(&rec, -100)                             // close_delta
	codec.WriteVarint(&rec, 500)                              // high_delta
	codec.WriteVarint(&rec, -300)                             // low_delta
	binary.Write(&rec, binary.LittleEndian, uint32(100))      // volume raw
	binary.Write(&rec, binary.LittleEndian, uint32(200))      // amount raw
	if index {
		binary.Write(&rec, binary.LittleEndian, uint16(1234)) // up_count
		binary.Write(&rec, binary.LittleEndian, uint16(567))  // down_count
	}
	body := make([]byte, 2+rec.Len())
	binary.LittleEndian.PutUint16(body[0:2], 1) // count
	copy(body[2:], rec.Bytes())
	return body
}

func TestDecodeKlinesStock(t *testing.T) {
	bars, err := decodeKlines(buildKlinesBody(t, false), 4, false)
	if err != nil {
		t.Fatalf("decodeKlines: %v", err)
	}
	if len(bars) != 1 {
		t.Fatalf("decodeKlines len = %d, want 1 (%+v)", len(bars), bars)
	}
	b := bars[0]
	if b.Open != 10.10 || b.Close != 10.00 || b.High != 10.60 || b.Low != 9.80 {
		t.Errorf("OHLC = %v/%v/%v/%v, want 10.10/10.00/10.60/9.80", b.Open, b.Close, b.High, b.Low)
	}
	if b.Time.Year() != 2026 || b.Time.Month() != 5 || b.Time.Day() != 27 {
		t.Errorf("Time = %v, want 2026-05-27", b.Time)
	}
	if want := codec.DecodeVolume(100) / 100; b.Volume != want {
		t.Errorf("Volume = %v, want %v", b.Volume, want)
	}
	if want := codec.DecodeVolume(200); b.Amount != want {
		t.Errorf("Amount = %v, want %v", b.Amount, want)
	}
	if b.UpCount != 0 || b.DownCount != 0 {
		t.Errorf("UpCount/DownCount = %d/%d, want 0/0", b.UpCount, b.DownCount)
	}
}

func TestDecodeKlinesIndex(t *testing.T) {
	bars, err := decodeKlines(buildKlinesBody(t, true), 4, true)
	if err != nil {
		t.Fatalf("decodeKlines: %v", err)
	}
	if len(bars) != 1 {
		t.Fatalf("decodeKlines len = %d, want 1 (%+v)", len(bars), bars)
	}
	b := bars[0]
	if b.UpCount != 1234 || b.DownCount != 567 {
		t.Errorf("UpCount/DownCount = %d/%d, want 1234/567", b.UpCount, b.DownCount)
	}
}

func TestDecodeKlinesTruncatedIndexRecord(t *testing.T) {
	// A truncated index record: 16 bytes (time u32 + 4 single-byte varints +
	// volume u32 + amount u32) but missing the trailing up/down u16 pair.
	// Must not panic: the record is dropped, returning empty bars and nil error.
	body := make([]byte, 2+16)
	binary.LittleEndian.PutUint16(body[0:2], 1) // count = 1
	off := 2
	binary.LittleEndian.PutUint32(body[off:off+4], 20260527) // time yyyymmdd
	off += 4
	for _, v := range []int64{1, 1, 1, 1} { // 4 single-byte price varints
		body[off] = byte(v)
		off++
	}
	binary.LittleEndian.PutUint32(body[off:off+4], 100)   // volume raw
	binary.LittleEndian.PutUint32(body[off+4:off+8], 200) // amount raw

	bars, err := decodeKlines(body, 4, true)
	if err != nil {
		t.Fatalf("decodeKlines: %v", err)
	}
	if len(bars) != 0 {
		t.Fatalf("decodeKlines = %+v, want empty (truncated index record dropped)", bars)
	}
}

func TestDecodeKlinesShortBody(t *testing.T) {
	for _, n := range []int{0, 1} {
		if _, err := decodeKlines(make([]byte, n), 4, false); err == nil {
			t.Errorf("decodeKlines(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestBarsMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Bars().Get("sz300308", Day, 0, 420); err == nil {
		t.Error("Get error = nil, want transport error")
	}
	if _, err := c.Bars().GetIndex("sz000001", Day, 0, 420); err == nil {
		t.Error("GetIndex error = nil, want transport error")
	}
}
