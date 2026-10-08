package go2tdx

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go2tdx/internal/frame"
)

// quotesMeta reads the top-level command_code / message_id fields of a frozen
// quotes fixture's metadata.json.
type quotesMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readQuotesMeta(t *testing.T, cmd string) quotesMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, "normal", "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m quotesMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

func mustNormalizeQuotes(t *testing.T, codes ...string) []quoteCode {
	t.Helper()
	norm, err := normalizeQuoteCodes(codes)
	if err != nil {
		t.Fatalf("normalizeQuoteCodes: %v", err)
	}
	return norm
}

// --- encode ---

func TestEncodeLegacyQuotesRequest(t *testing.T) {
	meta := readQuotesMeta(t, "legacy_quotes")
	if meta.CommandCode != 1342 {
		t.Errorf("legacy_quotes command_code = %d, want 1342", meta.CommandCode)
	}
	got := frame.EncodeRequest(meta.MessageID, msgLegacyQuotes, encodeQuoteCodeList(mustNormalizeQuotes(t, "sz000001")))
	want := sessionFixture(t, "legacy_quotes", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy_quotes request = % x, want % x", got, want)
	}
}

func TestEncodeSnapshotsRequest(t *testing.T) {
	meta := readQuotesMeta(t, "snapshots")
	if meta.CommandCode != 1356 {
		t.Errorf("snapshots command_code = %d, want 1356", meta.CommandCode)
	}
	got := frame.EncodeRequest(meta.MessageID, msgSnapshots, encodeQuoteCodeList(mustNormalizeQuotes(t, "sz000001")))
	want := sessionFixture(t, "snapshots", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("snapshots request = % x, want % x", got, want)
	}
}

func TestEncodeRefreshStreamRequest(t *testing.T) {
	meta := readQuotesMeta(t, "refresh_stream")
	if meta.CommandCode != 1351 {
		t.Errorf("refresh_stream command_code = %d, want 1351", meta.CommandCode)
	}
	got := frame.EncodeRequest(meta.MessageID, msgRefreshStream, encodeRefreshList(mustNormalizeQuotes(t, "sz000001")))
	want := sessionFixture(t, "refresh_stream", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("refresh_stream request = % x, want % x", got, want)
	}
}

func TestEncodeCategoryQuotesRequest(t *testing.T) {
	meta := readQuotesMeta(t, "category_quotes")
	if meta.CommandCode != 1355 {
		t.Errorf("category_quotes command_code = %d, want 1355", meta.CommandCode)
	}
	got := frame.EncodeRequest(meta.MessageID, msgCategoryQuotes, encodeCategoryRequest(6, 0x0000, 0, 80))
	want := sessionFixture(t, "category_quotes", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("category_quotes request = % x, want % x", got, want)
	}
}

// --- decode ---

func TestDecodeLegacyQuotesFixture(t *testing.T) {
	body := decodeFixtureBody(t, "legacy_quotes", "normal", msgLegacyQuotes)
	quotes, err := decodeLegacy(body, mustNormalizeQuotes(t, "sz000001"))
	if err != nil {
		t.Fatalf("decodeLegacy: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("decodeLegacy len = %d, want 1", len(quotes))
	}
	q := quotes[0]
	if q.Market != MarketSZ || q.Code != "000001" {
		t.Errorf("Market/Code = %d/%q, want 0/000001", q.Market, q.Code)
	}
	if q.Active1 != 7 {
		t.Errorf("Active1 = %d, want 7", q.Active1)
	}
	if q.Last != 10.14 {
		t.Errorf("Last = %v, want 10.14", q.Last)
	}
	if q.PreClose != 10.0 {
		t.Errorf("PreClose = %v, want 10.0", q.PreClose)
	}
	if q.Open != 10.13 {
		t.Errorf("Open = %v, want 10.13", q.Open)
	}
	if q.High != 10.2 {
		t.Errorf("High = %v, want 10.2", q.High)
	}
	if q.Low != 10.04 {
		t.Errorf("Low = %v, want 10.04", q.Low)
	}
	if q.TotalHand != 1000 || q.CurrentHand != 15 {
		t.Errorf("TotalHand/CurrentHand = %d/%d, want 1000/15", q.TotalHand, q.CurrentHand)
	}
	if q.InsideDish != 400 || q.OuterDisc != 600 || q.OpenAmount != 100 {
		t.Errorf("InsideDish/OuterDisc/OpenAmount = %v/%v/%v, want 400/600/100", q.InsideDish, q.OuterDisc, q.OpenAmount)
	}
	if q.Bids[0].Price != 10.13 || q.Bids[0].Volume != 320 {
		t.Errorf("Bids[0] = %+v, want {10.13 320}", q.Bids[0])
	}
	if q.Bids[4].Price != 10.09 || q.Bids[4].Volume != 66 {
		t.Errorf("Bids[4] = %+v, want {10.09 66}", q.Bids[4])
	}
	if q.Asks[0].Price != 10.14 || q.Asks[0].Volume != 428 {
		t.Errorf("Asks[0] = %+v, want {10.14 428}", q.Asks[0])
	}
	if q.Asks[4].Price != 10.18 || q.Asks[4].Volume != 71 {
		t.Errorf("Asks[4] = %+v, want {10.18 71}", q.Asks[4])
	}
}

func TestDecodeSnapshotsFixture(t *testing.T) {
	body := decodeFixtureBody(t, "snapshots", "normal", msgSnapshots)
	quotes, err := decodeSnapshots(body, mustNormalizeQuotes(t, "sz000001"))
	if err != nil {
		t.Fatalf("decodeSnapshots: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("decodeSnapshots len = %d, want 1", len(quotes))
	}
	q := quotes[0]
	if q.Code != "000001" || q.Active1 != 4582 {
		t.Errorf("Code/Active1 = %q/%d, want 000001/4582", q.Code, q.Active1)
	}
	if q.Last != 10.93 || q.PreClose != 10.66 || q.Open != 10.65 || q.High != 10.93 || q.Low != 10.62 {
		t.Errorf("prices = %v/%v/%v/%v/%v, want 10.93/10.66/10.65/10.93/10.62", q.Last, q.PreClose, q.Open, q.High, q.Low)
	}
	if q.Bids[0].Price != 10.92 || q.Bids[0].Volume != 1232 {
		t.Errorf("Bids[0] = %+v, want {10.92 1232}", q.Bids[0])
	}
	if q.Asks[0].Price != 10.93 || q.Asks[0].Volume != 12481 {
		t.Errorf("Asks[0] = %+v, want {10.93 12481}", q.Asks[0])
	}
}

func TestDecodeRefreshStreamFixture(t *testing.T) {
	body := decodeFixtureBody(t, "refresh_stream", "normal", msgRefreshStream)
	if got := hex.EncodeToString(body); got != "9393" {
		t.Errorf("raw refresh payload = %q, want 9393", got)
	}
	quotes, err := decodeRefresh(body, mustNormalizeQuotes(t, "sz000001"))
	if err != nil {
		t.Fatalf("decodeRefresh: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("decodeRefresh len = %d, want 0", len(quotes))
	}
}

func TestDecodeCategoryQuotesFixture(t *testing.T) {
	body := decodeFixtureBody(t, "category_quotes", "normal", msgCategoryQuotes)
	quotes, err := decodeCategory(body)
	if err != nil {
		t.Fatalf("decodeCategory: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("decodeCategory len = %d, want 0", len(quotes))
	}
}

func TestDecodeQuotesShortBody(t *testing.T) {
	if _, err := decodeSnapshots(make([]byte, 3), nil); err == nil {
		t.Error("decodeSnapshots(3B) error = nil, want errShortBody")
	}
	if _, err := decodeLegacy(make([]byte, 3), nil); err == nil {
		t.Error("decodeLegacy(3B) error = nil, want errShortBody")
	}
	if _, err := decodeRefresh(nil, nil); err == nil {
		t.Error("decodeRefresh(nil) error = nil, want errShortBody")
	}
	if _, err := decodeCategory(make([]byte, 3)); err == nil {
		t.Error("decodeCategory(3B) error = nil, want errShortBody")
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestQuotesMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Quotes().Snapshots([]string{"sz000001"}); err == nil {
		t.Error("Snapshots error = nil, want transport error")
	}
	if _, err := c.Quotes().Legacy([]string{"sz000001"}); err == nil {
		t.Error("Legacy error = nil, want transport error")
	}
	if _, err := c.Quotes().Refresh([]string{"sz000001"}); err == nil {
		t.Error("Refresh error = nil, want transport error")
	}
	if _, err := c.Quotes().Category(6, 0, 0, 80); err == nil {
		t.Error("Category error = nil, want transport error")
	}
}
