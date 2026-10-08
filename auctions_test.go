package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go2tdx/internal/frame"
)

// auctionsMeta reads the top-level command_code / message_id fields of a frozen
// auctions fixture's metadata.json.
type auctionsMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readAuctionsMeta(t *testing.T, cmd string) auctionsMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, "normal", "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m auctionsMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// --- encode ---

func TestEncodeAuctionSeriesRequest(t *testing.T) {
	meta := readAuctionsMeta(t, "auction_series")
	if meta.CommandCode != 1386 {
		t.Errorf("auction_series command_code = %d, want 1386", meta.CommandCode)
	}
	// Request data (28B): market u8 | 0 | code 6B | trading_date u32 (0=today) |
	// mode u32 (3) | 4B reserved | start u32 | limit u32 (500).
	data := make([]byte, 28)
	data[0] = byte(MarketSZ)
	copy(data[2:8], "000988")
	binary.LittleEndian.PutUint32(data[8:12], 0)    // trading_date: 0 = today
	binary.LittleEndian.PutUint32(data[12:16], 3)   // mode
	binary.LittleEndian.PutUint32(data[20:24], 0)   // start
	binary.LittleEndian.PutUint32(data[24:28], 500) // limit

	got := frame.EncodeRequest(meta.MessageID, msgAuctionSeries, data)
	want := sessionFixture(t, "auction_series", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("auction_series request = % x, want % x", got, want)
	}
}

// --- decode ---

func TestDecodeAuctionSeriesFixture(t *testing.T) {
	body := decodeFixtureBody(t, "auction_series", "normal", msgAuctionSeries)
	// date "" (today) maps to trading_date 0, so times keep the zero date.
	points, err := decodeAuctions(body, 0)
	if err != nil {
		t.Fatalf("decodeAuctions: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("decodeAuctions len = %d, want 1 (%+v)", len(points), points)
	}
	p := points[0]
	// expected.json records price as the raw f32 (f64_bits 406443d700000000 =
	// 162.1199951171875), matched_volume 2568, unmatched_signed_raw 2433,
	// second_raw 0, minute_of_day_raw 555 (09:15:00).
	if p.Price != 162.1199951171875 {
		t.Errorf("points[0].Price = %v, want 162.1199951171875", p.Price)
	}
	if p.MatchedVolume != 2568 {
		t.Errorf("points[0].MatchedVolume = %d, want 2568", p.MatchedVolume)
	}
	if p.Unmatched != 2433 {
		t.Errorf("points[0].Unmatched = %d, want 2433", p.Unmatched)
	}
	wantTime := time.Date(1, 1, 1, 9, 15, 0, 0, time.UTC)
	if !p.Time.Equal(wantTime) {
		t.Errorf("points[0].Time = %v, want %v", p.Time, wantTime)
	}
}

func TestDecodeAuctionSeriesWithDate(t *testing.T) {
	body := decodeFixtureBody(t, "auction_series", "normal", msgAuctionSeries)
	points, err := decodeAuctions(body, 20260511)
	if err != nil {
		t.Fatalf("decodeAuctions: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("decodeAuctions len = %d, want 1", len(points))
	}
	wantTime := time.Date(2026, 5, 11, 9, 15, 0, 0, time.UTC)
	if !points[0].Time.Equal(wantTime) {
		t.Errorf("points[0].Time = %v, want %v", points[0].Time, wantTime)
	}
}

func TestDecodeAuctionSeriesEmpty(t *testing.T) {
	points, err := decodeAuctions([]byte{0x00, 0x00}, 0)
	if err != nil {
		t.Fatalf("decodeAuctions(empty): %v", err)
	}
	if len(points) != 0 {
		t.Errorf("decodeAuctions(empty) len = %d, want 0", len(points))
	}
}

func TestDecodeAuctionSeriesTruncatedRecord(t *testing.T) {
	// count says 1 but only 15 of the 16 record bytes are present: the partial
	// trailing record is dropped, not an error (house convention).
	body := make([]byte, 2+15)
	binary.LittleEndian.PutUint16(body[0:2], 1)
	points, err := decodeAuctions(body, 0)
	if err != nil {
		t.Fatalf("decodeAuctions(truncated): %v", err)
	}
	if len(points) != 0 {
		t.Errorf("decodeAuctions(truncated) len = %d, want 0", len(points))
	}
}

func TestDecodeAuctionsShortBody(t *testing.T) {
	if _, err := decodeAuctions(make([]byte, 1), 0); err == nil {
		t.Error("decodeAuctions(1B) error = nil, want errShortBody")
	}
}

// --- public method against a non-listening pool: only the error path is reachable offline ---

func TestAuctionSeriesErrors(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	// Bad code / bad date fail before any network round trip.
	if _, err := c.Auctions().Series("nope", ""); err == nil {
		t.Error("Series(bad code) error = nil, want error")
	}
	if _, err := c.Auctions().Series("sz000988", "2026-13-45x"); err == nil {
		t.Error("Series(bad date) error = nil, want error")
	}
	// Valid arguments reach the (dead) transport and propagate its error.
	if _, err := c.Auctions().Series("sz000988", ""); err == nil {
		t.Error("Series error = nil, want transport error")
	}
}
