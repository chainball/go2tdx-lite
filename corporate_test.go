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
)

// corporateMeta reads the top-level command_code / message_id fields of a frozen
// corporate fixture's metadata.json.
type corporateMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readCorporateMeta(t *testing.T, cmd, sub string) corporateMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, sub, "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m corporateMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// --- encode ---

func TestEncodeCapitalChangesRequest(t *testing.T) {
	meta := readCorporateMeta(t, "capital_changes", "normal")
	if meta.CommandCode != 15 {
		t.Errorf("capital_changes/normal command_code = %d, want 15", meta.CommandCode)
	}

	data := encodeCorporateRequestNoErr(t, []string{"sz000001"})
	got := frame.EncodeRequest(meta.MessageID, 15, data)
	want := sessionFixture(t, "capital_changes", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("capital_changes/normal request = % x, want % x", got, want)
	}
}

func TestEncodeFinanceBatchRequest(t *testing.T) {
	meta := readCorporateMeta(t, "finance_batch", "normal")
	if meta.CommandCode != 16 {
		t.Errorf("finance_batch/normal command_code = %d, want 16", meta.CommandCode)
	}

	data := encodeCorporateRequestNoErr(t, []string{"sz000001"})
	got := frame.EncodeRequest(meta.MessageID, 16, data)
	want := sessionFixture(t, "finance_batch", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("finance_batch/normal request = % x, want % x", got, want)
	}
}

func encodeCorporateRequestNoErr(t *testing.T, codes []string) []byte {
	t.Helper()
	data, err := encodeCorporateRequest(codes)
	if err != nil {
		t.Fatalf("encodeCorporateRequest: %v", err)
	}
	return data
}

// --- decode: capital_changes ---

func TestDecodeCapitalChanges(t *testing.T) {
	// Synthetic body: block_count=1, header (market=0, code=000001, record_count=1),
	// record (market=0, code=000001, reserved=0, date=20260511, category=15, c1..c4=0).
	body := make([]byte, 2+9+29)
	binary.LittleEndian.PutUint16(body[0:2], 1) // block_count
	off := 2
	body[off] = 0                                       // header market
	copy(body[off+1:off+7], "000001")                   // header code
	binary.LittleEndian.PutUint16(body[off+7:off+9], 1) // record_count
	off += 9
	body[off] = 0                                               // record market
	copy(body[off+1:off+7], "000001")                           // record code
	body[off+7] = 0                                             // reserved_7
	binary.LittleEndian.PutUint32(body[off+8:off+12], 20260511) // date
	body[off+12] = 15                                           // category

	got, err := decodeCapitalChanges(body)
	if err != nil {
		t.Fatalf("decodeCapitalChanges: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decodeCapitalChanges len = %d, want 1 (%+v)", len(got), got)
	}
	want := XdxrRecord{Market: 0, Code: "000001", Date: "20260511", Category: 15}
	if got[0] != want {
		t.Fatalf("decodeCapitalChanges = %+v, want %+v", got[0], want)
	}
}

func TestDecodeCapitalChangesFixture(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "capital_changes", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if resp.MsgType != 15 {
		t.Errorf("msg_type = %d, want 15", resp.MsgType)
	}
	got, err := decodeCapitalChanges(resp.Body)
	if err != nil {
		t.Fatalf("decodeCapitalChanges: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decodeCapitalChanges len = %d, want 1 (%+v)", len(got), got)
	}
	r := got[0]
	if r.Market != 0 || r.Code != "000001" || r.Date != "20260511" || r.Category != 15 {
		t.Errorf("decodeCapitalChanges = %+v, want market=0 code=000001 date=20260511 category=15", r)
	}
	// category 15 is raw: c3 f32 0x40600000 = 3.5 stays unscaled.
	if r.C3 != 3.5 {
		t.Errorf("C3 = %v, want 3.5", r.C3)
	}
}

func TestDecodeCapitalChangesShortBody(t *testing.T) {
	for _, n := range []int{0, 1} {
		if _, err := decodeCapitalChanges(make([]byte, n)); err == nil {
			t.Errorf("decodeCapitalChanges(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- decode: finance_batch ---

func TestDecodeFinanceBatch(t *testing.T) {
	// Synthetic body: count=1 + 143B record (market=0, code=000001, 136B finance_info).
	rec := make([]byte, 143)
	rec[0] = 0 // market
	copy(rec[1:7], "000001")
	fin := rec[7:]
	binary.LittleEndian.PutUint32(fin[0:4], math.Float32bits(100.0)) // liu_tong_gu_ben
	binary.LittleEndian.PutUint16(fin[4:6], 1)                       // province
	binary.LittleEndian.PutUint16(fin[6:8], 2)                       // industry
	binary.LittleEndian.PutUint32(fin[8:12], 20260519)               // updated_date
	binary.LittleEndian.PutUint32(fin[12:16], 19910103)              // ipo_date
	// fin[16:136] stays zero: 30 x f32.

	body := make([]byte, 2+143)
	binary.LittleEndian.PutUint16(body[0:2], 1) // count
	copy(body[2:], rec)

	got, err := decodeFinanceBatch(body)
	if err != nil {
		t.Fatalf("decodeFinanceBatch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decodeFinanceBatch len = %d, want 1 (%+v)", len(got), got)
	}
	f := got[0]
	if f.Market != 0 || f.Code != "000001" {
		t.Errorf("Market/Code = %d/%q, want 0/000001", f.Market, f.Code)
	}
	if f.LiuTongGuBen != 100.0 || f.Province != 1 || f.Industry != 2 {
		t.Errorf("LiuTongGuBen/Province/Industry = %v/%d/%d, want 100/1/2", f.LiuTongGuBen, f.Province, f.Industry)
	}
	if f.UpdatedDate != "20260519" || f.IPODate != "19910103" {
		t.Errorf("UpdatedDate/IPODate = %q/%q, want 20260519/19910103", f.UpdatedDate, f.IPODate)
	}
}

func TestDecodeFinanceBatchFixture(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "finance_batch", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if resp.MsgType != 16 {
		t.Errorf("msg_type = %d, want 16", resp.MsgType)
	}
	got, err := decodeFinanceBatch(resp.Body)
	if err != nil {
		t.Fatalf("decodeFinanceBatch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decodeFinanceBatch len = %d, want 1 (%+v)", len(got), got)
	}
	f := got[0]
	if f.Code != "000001" || f.LiuTongGuBen != 100.0 {
		t.Errorf("Code/LiuTongGuBen = %q/%v, want 000001/100", f.Code, f.LiuTongGuBen)
	}
	if f.Province != 1 || f.Industry != 2 {
		t.Errorf("Province/Industry = %d/%d, want 1/2", f.Province, f.Industry)
	}
	if f.UpdatedDate != "20260425" || f.IPODate != "19910403" {
		t.Errorf("UpdatedDate/IPODate = %q/%q, want 20260425/19910403", f.UpdatedDate, f.IPODate)
	}
}

func TestDecodeFinanceBatchShortBody(t *testing.T) {
	for _, n := range []int{0, 1} {
		if _, err := decodeFinanceBatch(make([]byte, n)); err == nil {
			t.Errorf("decodeFinanceBatch(%d bytes) error = nil, want errShortBody", n)
		}
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestCorporateMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Corporate().CapitalChanges([]string{"sz000001"}); err == nil {
		t.Error("CapitalChanges error = nil, want transport error")
	}
	if _, err := c.Corporate().FinanceBatch([]string{"sz000001"}); err == nil {
		t.Error("FinanceBatch error = nil, want transport error")
	}
}
