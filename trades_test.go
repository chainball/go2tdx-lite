package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chainball/go2tdx-lite/internal/frame"
)

// tradesMeta reads the top-level command_code / message_id fields of a frozen
// trades fixture's metadata.json.
type tradesMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readTradesMeta(t *testing.T, cmd string) tradesMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, "normal", "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m tradesMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// --- encode ---

func TestEncodeTodayTicksRequest(t *testing.T) {
	meta := readTradesMeta(t, "today_ticks")
	if meta.CommandCode != 4037 {
		t.Errorf("today_ticks command_code = %d, want 4037", meta.CommandCode)
	}
	// Request data (12B): market u16 | code 6B | start u16 | count u16.
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], MarketSZ)
	copy(data[2:8], "000001")
	binary.LittleEndian.PutUint16(data[8:10], 0)
	binary.LittleEndian.PutUint16(data[10:12], 1800)

	got := frame.EncodeRequest(meta.MessageID, msgTodayTicks, data)
	want := sessionFixture(t, "today_ticks", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("today_ticks request = % x, want % x", got, want)
	}
}

func TestEncodeHistoricalTicksRequest(t *testing.T) {
	meta := readTradesMeta(t, "historical_ticks")
	if meta.CommandCode != 4038 {
		t.Errorf("historical_ticks command_code = %d, want 4038", meta.CommandCode)
	}
	// Request data (16B): trading_date u32 | market u16 | code 6B | start u16 | count u16.
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], 20260511)
	binary.LittleEndian.PutUint16(data[4:6], MarketSZ)
	copy(data[6:12], "300308")
	binary.LittleEndian.PutUint16(data[12:14], 0)
	binary.LittleEndian.PutUint16(data[14:16], 1800)

	got := frame.EncodeRequest(meta.MessageID, msgHistoricalTicks, data)
	want := sessionFixture(t, "historical_ticks", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("historical_ticks request = % x, want % x", got, want)
	}
}

// --- decode ---

func TestDecodeTodayTicksFixture(t *testing.T) {
	body := decodeFixtureBody(t, "today_ticks", "normal", msgTodayTicks)
	ticks, err := decodeTodayTicks(body)
	if err != nil {
		t.Fatalf("decodeTodayTicks: %v", err)
	}
	if len(ticks) != 1 {
		t.Fatalf("decodeTodayTicks len = %d, want 1 (%+v)", len(ticks), ticks)
	}
	tk := ticks[0]
	if tk.Price != 0.1 {
		t.Errorf("ticks[0].Price = %v, want 0.1", tk.Price)
	}
	if tk.Volume != 20 {
		t.Errorf("ticks[0].Volume = %d, want 20", tk.Volume)
	}
	if tk.OrderCount != 3 {
		t.Errorf("ticks[0].OrderCount = %d, want 3", tk.OrderCount)
	}
	if tk.Status != 0 {
		t.Errorf("ticks[0].Status = %d, want 0", tk.Status)
	}
	wantTime := time.Date(1, 1, 1, 14, 8, 0, 0, time.UTC)
	if !tk.Time.Equal(wantTime) {
		t.Errorf("ticks[0].Time = %v, want %v", tk.Time, wantTime)
	}
}

func TestDecodeHistoricalTicksFixture(t *testing.T) {
	body := decodeFixtureBody(t, "historical_ticks", "normal", msgHistoricalTicks)
	ticks, priceBase, err := decodeHistoricalTicks(body, 20260511)
	if err != nil {
		t.Fatalf("decodeHistoricalTicks: %v", err)
	}
	if priceBase != 35.5 {
		t.Errorf("price_base = %v, want 35.5", priceBase)
	}
	if len(ticks) != 1 {
		t.Fatalf("decodeHistoricalTicks len = %d, want 1 (%+v)", len(ticks), ticks)
	}
	tk := ticks[0]
	if tk.Price != 0.1 {
		t.Errorf("ticks[0].Price = %v, want 0.1", tk.Price)
	}
	if tk.Volume != 20 {
		t.Errorf("ticks[0].Volume = %d, want 20", tk.Volume)
	}
	if tk.OrderCount != 3 {
		t.Errorf("ticks[0].OrderCount = %d, want 3", tk.OrderCount)
	}
	if tk.Status != 5 {
		t.Errorf("ticks[0].Status = %d, want 5", tk.Status)
	}
	wantTime := time.Date(2026, 5, 11, 14, 8, 0, 0, time.UTC)
	if !tk.Time.Equal(wantTime) {
		t.Errorf("ticks[0].Time = %v, want %v", tk.Time, wantTime)
	}
}

func TestDecodeTicksShortBody(t *testing.T) {
	if _, err := decodeTodayTicks(make([]byte, 1)); err == nil {
		t.Error("decodeTodayTicks(1B) error = nil, want errShortBody")
	}
	if _, _, err := decodeHistoricalTicks(make([]byte, 5), 20260511); err == nil {
		t.Error("decodeHistoricalTicks(5B) error = nil, want errShortBody")
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestTradesMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Trades().Today("sz000001", 0, 1800); err == nil {
		t.Error("Today error = nil, want transport error")
	}
	if _, err := c.Trades().History("sz300308", "20260511", 0, 1800); err == nil {
		t.Error("History error = nil, want transport error")
	}
}
