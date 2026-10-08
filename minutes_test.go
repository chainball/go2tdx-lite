package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go2tdx/internal/frame"
)

// minutesMeta reads the top-level command_code / message_id fields of a frozen
// minutes fixture's metadata.json.
type minutesMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

func readMinutesMeta(t *testing.T, cmd string) minutesMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, "normal", "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m minutesMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// decodeFixtureBody reads a frozen response.bin and returns its decoded body.
func decodeFixtureBody(t *testing.T, cmd, sub string, wantType uint16) []byte {
	t.Helper()
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, cmd, sub, "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes %s/%s: %v", cmd, sub, err)
	}
	if resp.MsgType != wantType {
		t.Errorf("%s/%s msg_type = %d, want %d", cmd, sub, resp.MsgType, wantType)
	}
	return resp.Body
}

// --- encode ---

func TestEncodeTodayIntradayRequest(t *testing.T) {
	meta := readMinutesMeta(t, "today_intraday")
	if meta.CommandCode != 1335 {
		t.Errorf("today_intraday command_code = %d, want 1335", meta.CommandCode)
	}
	// Request data (12B): market u16 | code 6B | reserved_tail 4B (00 00 00 93).
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], MarketSZ)
	copy(data[2:8], "000988")
	data[11] = 0x93

	got := frame.EncodeRequest(meta.MessageID, msgTodayIntraday, data)
	want := sessionFixture(t, "today_intraday", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("today_intraday request = % x, want % x", got, want)
	}
}

func TestEncodeHistoricalIntradayRequest(t *testing.T) {
	meta := readMinutesMeta(t, "historical_intraday")
	if meta.CommandCode != 4020 {
		t.Errorf("historical_intraday command_code = %d, want 4020", meta.CommandCode)
	}
	// Request data (11B): trading_date u32 | market u8 | code 6B.
	data := make([]byte, 11)
	binary.LittleEndian.PutUint32(data[0:4], 20260511)
	data[4] = byte(MarketSZ)
	copy(data[5:11], "300308")

	got := frame.EncodeRequest(meta.MessageID, msgHistoricalIntraday, data)
	want := sessionFixture(t, "historical_intraday", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("historical_intraday request = % x, want % x", got, want)
	}
}

func TestEncodeRecentIntradayRequest(t *testing.T) {
	meta := readMinutesMeta(t, "recent_intraday")
	if meta.CommandCode != 4075 {
		t.Errorf("recent_intraday command_code = %d, want 4075", meta.CommandCode)
	}
	selector := recentDateSelectorBase - uint32(pythonDateOrdinal(2026, 5, 11))
	if selector != 0xFECAD961 {
		t.Fatalf("date_selector = %#x, want 0xFECAD961", selector)
	}
	// Request data (11B): date_selector u32 | market u8 | code 6B.
	data := make([]byte, 11)
	binary.LittleEndian.PutUint32(data[0:4], selector)
	data[4] = byte(MarketSZ)
	copy(data[5:11], "300308")

	got := frame.EncodeRequest(meta.MessageID, msgRecentIntraday, data)
	want := sessionFixture(t, "recent_intraday", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("recent_intraday request = % x, want % x", got, want)
	}
}

func TestEncodeIntradayAuxRequest(t *testing.T) {
	meta := readMinutesMeta(t, "intraday_aux")
	if meta.CommandCode != 1307 {
		t.Errorf("intraday_aux command_code = %d, want 1307", meta.CommandCode)
	}
	// Request data (28B): market u16 | code 6B | 19×0 | kind u8.
	data := make([]byte, 28)
	binary.LittleEndian.PutUint16(data[0:2], MarketSZ)
	copy(data[2:8], "000988")
	data[27] = AuxBuySellStrength

	got := frame.EncodeRequest(meta.MessageID, msgIntradayAux, data)
	want := sessionFixture(t, "intraday_aux", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("intraday_aux request = % x, want % x", got, want)
	}
}

func TestEncodeSparklineRequest(t *testing.T) {
	meta := readMinutesMeta(t, "sparkline")
	if meta.CommandCode != 4049 {
		t.Errorf("sparkline command_code = %d, want 4049", meta.CommandCode)
	}
	// Request data (37B): market u8 | 0 | code 6B | 16×0 | selector u8 (1) | 0 |
	// window u16 (20) | fixed u32 (0x01000000) | 5×0.
	data := make([]byte, 37)
	data[0] = byte(MarketSZ)
	copy(data[2:8], "000001")
	data[24] = 1
	binary.LittleEndian.PutUint16(data[26:28], 20)
	binary.LittleEndian.PutUint32(data[28:32], 0x01000000)

	got := frame.EncodeRequest(meta.MessageID, msgSparkline, data)
	want := sessionFixture(t, "sparkline", "normal", "request.bin")
	if !bytes.Equal(got, want) {
		t.Fatalf("sparkline request = % x, want % x", got, want)
	}
}

// --- decode ---

func TestDecodeIntradayAux(t *testing.T) {
	// Synthetic body: count=1 + series_a varint=5 + series_b varint=6.
	body := []byte{1, 0, 5, 6}
	points, err := decodeIntradayAux(body, AuxBuySellStrength)
	if err != nil {
		t.Fatalf("decodeIntradayAux: %v", err)
	}
	if len(points) != 1 || points[0] != (AuxPoint{SeriesA: 5, SeriesB: 6}) {
		t.Fatalf("decodeIntradayAux = %+v, want [{SeriesA:5 SeriesB:6}]", points)
	}
}

func TestDecodeIntradayAuxVolumeComparison(t *testing.T) {
	// Synthetic body: count=1 + 8B (previous_day f32=100.5 + current_day f32=120.5).
	body := make([]byte, 10)
	binary.LittleEndian.PutUint16(body[0:2], 1)
	binary.LittleEndian.PutUint32(body[2:6], 0x42C90000)  // 100.5 f32
	binary.LittleEndian.PutUint32(body[6:10], 0x42F10000) // 120.5 f32
	points, err := decodeIntradayAux(body, AuxVolumeComparison)
	if err != nil {
		t.Fatalf("decodeIntradayAux: %v", err)
	}
	if len(points) != 1 || points[0].SeriesA != 100 || points[0].SeriesB != 120 {
		t.Fatalf("decodeIntradayAux = %+v, want [{SeriesA:100 SeriesB:120}]", points)
	}
}

func TestDecodeSparklineFixture(t *testing.T) {
	body := decodeFixtureBody(t, "sparkline", "normal", msgSparkline)
	base, prices, err := decodeSparkline(body)
	if err != nil {
		t.Fatalf("decodeSparkline: %v", err)
	}
	if base != 10.0 {
		t.Errorf("base_price = %v, want 10.0", base)
	}
	if len(prices) != 2 || prices[0] != 10.0 || prices[1] != float64(float32(10.1)) {
		t.Errorf("prices = %v, want [10.0 %v]", prices, float64(float32(10.1)))
	}
}

func TestDecodeRecentIntradayFixture(t *testing.T) {
	body := decodeFixtureBody(t, "recent_intraday", "normal", msgRecentIntraday)
	bars, prevClose, open, err := decodeRecentIntraday(body, minutePriceDivisor("300308"))
	if err != nil {
		t.Fatalf("decodeRecentIntraday: %v", err)
	}
	if prevClose != 10.0 {
		t.Errorf("prev_close = %v, want 10.0", prevClose)
	}
	if open != float64(float32(10.1)) {
		t.Errorf("open_price = %v, want %v", open, float64(float32(10.1)))
	}
	if len(bars) != 1 {
		t.Fatalf("decodeRecentIntraday len = %d, want 1 (%+v)", len(bars), bars)
	}
	b := bars[0]
	if b.Price != 0.1 || b.Avg != 0.0011 || b.Volume != 12 {
		t.Errorf("bar = %+v, want {Price:0.1 Avg:0.0011 Volume:12}", b)
	}
}

func TestDecodeRecentIntradayEmptyFixture(t *testing.T) {
	body := decodeFixtureBody(t, "recent_intraday", "include_raw_false", msgRecentIntraday)
	bars, prevClose, open, err := decodeRecentIntraday(body, minutePriceDivisor("300308"))
	if err != nil {
		t.Fatalf("decodeRecentIntraday: %v", err)
	}
	if prevClose != 10.0 || open != float64(float32(10.1)) {
		t.Errorf("prev_close/open = %v/%v, want 10.0/%v", prevClose, open, float64(float32(10.1)))
	}
	if len(bars) != 0 {
		t.Fatalf("decodeRecentIntraday len = %d, want 0 (%+v)", len(bars), bars)
	}
}

func TestDecodeTodayIntradayFixture(t *testing.T) {
	body := decodeFixtureBody(t, "today_intraday", "normal", msgTodayIntraday)
	bars, reservedZero, err := decodeTodayIntraday(body, minutePriceDivisor("000988"))
	if err != nil {
		t.Fatalf("decodeTodayIntraday: %v", err)
	}
	if reservedZero != 0 {
		t.Errorf("reserved_zero = %d, want 0", reservedZero)
	}
	if len(bars) != 0 {
		t.Fatalf("decodeTodayIntraday len = %d, want 0 (%+v)", len(bars), bars)
	}
}

func TestDecodeHistoricalIntradayFixture(t *testing.T) {
	body := decodeFixtureBody(t, "historical_intraday", "normal", msgHistoricalIntraday)
	bars, prevClose, err := decodeHistoricalIntraday(body, minutePriceDivisor("300308"))
	if err != nil {
		t.Fatalf("decodeHistoricalIntraday: %v", err)
	}
	if prevClose != 10.0 {
		t.Errorf("prev_close = %v, want 10.0", prevClose)
	}
	if len(bars) != 0 {
		t.Fatalf("decodeHistoricalIntraday len = %d, want 0 (%+v)", len(bars), bars)
	}
}

// --- unit semantics ---

func TestPythonDateOrdinal(t *testing.T) {
	if got := pythonDateOrdinal(2026, 5, 11); got != 739747 {
		t.Errorf("pythonDateOrdinal(2026,5,11) = %d, want 739747", got)
	}
	if got := pythonDateOrdinal(1, 1, 1); got != 1 {
		t.Errorf("pythonDateOrdinal(1,1,1) = %d, want 1", got)
	}
}

func TestMinutePriceDivisor(t *testing.T) {
	for _, code := range []string{"000001", "300308", "600000", "920001", "000988"} {
		if got := minutePriceDivisor(code); got != 1 {
			t.Errorf("minutePriceDivisor(%s) = %d, want 1", code, got)
		}
	}
	for _, code := range []string{"510300", "159915", "513100", "560000", "580000"} {
		if got := minutePriceDivisor(code); got != 10 {
			t.Errorf("minutePriceDivisor(%s) = %d, want 10", code, got)
		}
	}
}

func TestParseDateParts(t *testing.T) {
	y, m, d, err := parseDateParts("20260511")
	if err != nil || y != 2026 || m != 5 || d != 11 {
		t.Errorf("parseDateParts(20260511) = %d/%d/%d/%v, want 2026/5/11/nil", y, m, d, err)
	}
	y, m, d, err = parseDateParts("2026-05-11")
	if err != nil || y != 2026 || m != 5 || d != 11 {
		t.Errorf("parseDateParts(2026-05-11) = %d/%d/%d/%v, want 2026/5/11/nil", y, m, d, err)
	}
	for _, bad := range []string{"", "2026", "2026-5-11", "2026051x", "20261311"} {
		if _, _, _, err := parseDateParts(bad); err == nil {
			t.Errorf("parseDateParts(%q) error = nil, want error", bad)
		}
	}
}

func TestDecodeMinuteShortBody(t *testing.T) {
	if _, _, err := decodeTodayIntraday(make([]byte, 3), 1); err == nil {
		t.Error("decodeTodayIntraday(3B) error = nil, want errShortBody")
	}
	if _, _, err := decodeHistoricalIntraday(make([]byte, 5), 1); err == nil {
		t.Error("decodeHistoricalIntraday(5B) error = nil, want errShortBody")
	}
	if _, _, _, err := decodeRecentIntraday(make([]byte, 9), 1); err == nil {
		t.Error("decodeRecentIntraday(9B) error = nil, want errShortBody")
	}
	if _, _, err := decodeSparkline(make([]byte, 41)); err == nil {
		t.Error("decodeSparkline(41B) error = nil, want errShortBody")
	}
}

// --- public methods against a non-listening pool: only the error path is reachable offline ---

func TestMinutesMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Minutes().Today("sz000001"); err == nil {
		t.Error("Today error = nil, want transport error")
	}
	if _, err := c.Minutes().History("sz300308", "20260511"); err == nil {
		t.Error("History error = nil, want transport error")
	}
	if _, err := c.Minutes().Recent("sz300308", "20260511"); err == nil {
		t.Error("Recent error = nil, want transport error")
	}
	if _, err := c.Minutes().Aux("sz000988", AuxBuySellStrength); err == nil {
		t.Error("Aux error = nil, want transport error")
	}
	if _, err := c.Minutes().Sparkline("sz000001"); err == nil {
		t.Error("Sparkline error = nil, want transport error")
	}
}
