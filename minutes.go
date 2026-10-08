package go2tdx

import (
	"encoding/binary"
	"fmt"

	"go2tdx/internal/codec"
)

const (
	msgTodayIntraday      uint16 = 0x0537 // 1335
	msgHistoricalIntraday uint16 = 0x0FB4 // 4020
	msgRecentIntraday     uint16 = 0x0FEB // 4075
	msgIntradayAux        uint16 = 0x051B // 1307
	msgSparkline          uint16 = 0x0FD1 // 4049
)

// Intraday auxiliary (分时副图) selectors for MinutesAPI.Aux.
const (
	AuxBuySellStrength  byte = 0x00 // 买卖强度
	AuxVolumeComparison byte = 0x0B // 量比
)

// recentDateSelectorBase is the fixed offset the server subtracts a Python date
// ordinal from to form the recent_intraday date_selector (0xFED62304).
const recentDateSelectorBase uint32 = 0xFED62304

// Today returns the current-day minute bars for a security.
// Request (12B): market u16 | code 6B | reserved_tail 4B (00 00 00 93).
func (a *MinutesAPI) Today(code string) ([]MinuteBar, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], market)
	copy(data[2:8], code6)
	data[11] = 0x93
	body, err := a.c.pool.Exec(msgTodayIntraday, data)
	if err != nil {
		return nil, err
	}
	bars, _, err := decodeTodayIntraday(body, minutePriceDivisor(code6))
	return bars, err
}

// History returns the minute bars for a past trading date.
// date is "yyyymmdd" (or "yyyy-mm-dd").
// Request (11B): trading_date u32 | market u8 | code 6B.
func (a *MinutesAPI) History(code, date string) ([]MinuteBar, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	ymd, err := parseDateYMD(date)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 11)
	binary.LittleEndian.PutUint32(data[0:4], ymd)
	data[4] = byte(market)
	copy(data[5:11], code6)
	body, err := a.c.pool.Exec(msgHistoricalIntraday, data)
	if err != nil {
		return nil, err
	}
	bars, _, err := decodeHistoricalIntraday(body, minutePriceDivisor(code6))
	return bars, err
}

// Recent returns the minute bars for a date within the server's recent window.
// date is "yyyymmdd" (or "yyyy-mm-dd").
// Request (11B): date_selector u32 | market u8 | code 6B.
func (a *MinutesAPI) Recent(code, date string) ([]MinuteBar, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	y, m, d, err := parseDateParts(date)
	if err != nil {
		return nil, err
	}
	selector := recentDateSelectorBase - uint32(pythonDateOrdinal(y, m, d))
	data := make([]byte, 11)
	binary.LittleEndian.PutUint32(data[0:4], selector)
	data[4] = byte(market)
	copy(data[5:11], code6)
	body, err := a.c.pool.Exec(msgRecentIntraday, data)
	if err != nil {
		return nil, err
	}
	bars, _, _, err := decodeRecentIntraday(body, minutePriceDivisor(code6))
	return bars, err
}

// Aux returns the intraday auxiliary series for a security.
// kind is AuxBuySellStrength (0x00, 买卖强度) or AuxVolumeComparison (0x0B, 量比).
// Request (28B): market u16 | code 6B | 19×0 | kind u8.
func (a *MinutesAPI) Aux(code string, kind byte) ([]AuxPoint, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 28)
	binary.LittleEndian.PutUint16(data[0:2], market)
	copy(data[2:8], code6)
	data[27] = kind
	body, err := a.c.pool.Exec(msgIntradayAux, data)
	if err != nil {
		return nil, err
	}
	return decodeIntradayAux(body, kind)
}

// Sparkline returns a lightweight intraday price series (小走势图).
// Request (37B): market u8 | 0 | code 6B | 16×0 | selector u8 (1) | 0 |
// window u16 (20) | fixed u32 (0x01000000) | 5×0.
// The returned slice is base_price followed by the price array.
func (a *MinutesAPI) Sparkline(code string) ([]float64, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 37)
	data[0] = byte(market)
	copy(data[2:8], code6)
	data[24] = 1                                           // selector
	binary.LittleEndian.PutUint16(data[26:28], 20)         // window
	binary.LittleEndian.PutUint32(data[28:32], 0x01000000) // fixed
	body, err := a.c.pool.Exec(msgSparkline, data)
	if err != nil {
		return nil, err
	}
	base, prices, err := decodeSparkline(body)
	if err != nil {
		return nil, err
	}
	out := make([]float64, 0, len(prices)+1)
	out = append(out, base)
	out = append(out, prices...)
	return out, nil
}

// decodeTodayIntraday decodes a today_intraday (0x0537) response body:
// 2B count u16 | 2B reserved_zero u16 | N records of 3 varints (price, avg,
// volume). Price and avg are absolute on the first record and relative to that
// first record afterwards.
func decodeTodayIntraday(body []byte, divisor int) ([]MinuteBar, uint16, error) {
	if len(body) < 4 {
		return nil, 0, errShortBody
	}
	reservedZero := binary.LittleEndian.Uint16(body[2:4])
	bars, err := decodeRelativeMinuteBars(body, 4, divisor)
	return bars, reservedZero, err
}

// decodeHistoricalIntraday decodes a historical_intraday (0x0FB4) response body:
// 2B count u16 | 4B prev_close f32 | N records of 3 varints (price_delta
// cumulative, aux_delta, volume). Only price and volume map onto MinuteBar; the
// aux_delta field carries no average-price semantics, so Avg stays 0.
func decodeHistoricalIntraday(body []byte, divisor int) ([]MinuteBar, float64, error) {
	if len(body) < 6 {
		return nil, 0, errShortBody
	}
	prevClose := f32At(body, 2)
	bars, err := decodeCumulativeMinuteBars(body, 6, divisor)
	return bars, prevClose, err
}

// decodeRecentIntraday decodes a recent_intraday (0x0FEB) response body:
// 2B count u16 | 4B prev_close f32 | 4B open_price f32 | N records of 3 varints
// (price, avg, volume; first record absolute, later relative to first).
func decodeRecentIntraday(body []byte, divisor int) ([]MinuteBar, float64, float64, error) {
	if len(body) < 10 {
		return nil, 0, 0, errShortBody
	}
	prevClose := f32At(body, 2)
	open := f32At(body, 6)
	bars, err := decodeRelativeMinuteBars(body, 10, divisor)
	return bars, prevClose, open, err
}

// decodeRelativeMinuteBars decodes minute records starting at off: each record
// is 3 varints (price, avg, volume). Price and avg use first-record-as-base
// semantics: the first record's value is absolute; later records are deltas
// added to it. A truncated trailing record is dropped.
func decodeRelativeMinuteBars(body []byte, off, divisor int) ([]MinuteBar, error) {
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	bars := make([]MinuteBar, 0, n)
	var basePrice, baseAvg int64
	for i := 0; i < n; i++ {
		price, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		avg, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		vol, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		if i == 0 {
			basePrice, baseAvg = price, avg
		} else {
			price += basePrice
			avg += baseAvg
		}
		bars = append(bars, MinuteBar{
			Price:  minutePrice(price, divisor),
			Avg:    minuteAvg(avg, divisor),
			Volume: float64(vol),
		})
	}
	return bars, nil
}

// decodeCumulativeMinuteBars decodes minute records starting at off: each record
// is 3 varints (price_delta, aux_delta, volume). price_delta accumulates onto a
// running price; aux_delta carries no average-price semantics here.
func decodeCumulativeMinuteBars(body []byte, off, divisor int) ([]MinuteBar, error) {
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	bars := make([]MinuteBar, 0, n)
	var priceAcc int64
	for i := 0; i < n; i++ {
		delta, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		if _, ok := readVarintSafe(body, &off); !ok { // aux_delta (unused)
			break
		}
		vol, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		priceAcc += delta
		bars = append(bars, MinuteBar{
			Price:  minutePrice(priceAcc, divisor),
			Volume: float64(vol),
		})
	}
	return bars, nil
}

// decodeIntradayAux decodes an intraday_aux (0x051B) response body: 2B count u16
// + N records. kind 0x00 → (series_a varint + series_b varint); kind 0x0B →
// 8B (previous_day f32 + current_day f32, truncated to int64).
func decodeIntradayAux(body []byte, kind byte) ([]AuxPoint, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	points := make([]AuxPoint, 0, n)
	off := 2
	if kind == AuxVolumeComparison {
		for i := 0; i < n; i++ {
			if off+8 > len(body) {
				break
			}
			prev := f32At(body, off)
			cur := f32At(body, off+4)
			off += 8
			points = append(points, AuxPoint{SeriesA: int64(prev), SeriesB: int64(cur)})
		}
		return points, nil
	}
	for i := 0; i < n; i++ {
		a, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		b, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		points = append(points, AuxPoint{SeriesA: a, SeriesB: b})
	}
	return points, nil
}

// decodeSparkline decodes a sparkline (0x0FD1) response body (≥42B):
// market_id u8 | reserved | code 6B | 16 reserved | selector_echo u8 |
// reserved | reserved_param u32 | reserved 4B | max_count u16 | base_price f32 |
// price_count u16 | price_count×f32.
func decodeSparkline(body []byte) (float64, []float64, error) {
	if len(body) < 42 {
		return 0, nil, errShortBody
	}
	basePrice := f32At(body, 36)
	count := int(binary.LittleEndian.Uint16(body[40:42]))
	if 42+count*4 > len(body) {
		return 0, nil, errShortBody
	}
	prices := make([]float64, 0, count)
	off := 42
	for i := 0; i < count; i++ {
		prices = append(prices, f32At(body, off))
		off += 4
	}
	return basePrice, prices, nil
}

// minutePrice converts a raw minute price field to yuan.
func minutePrice(raw int64, divisor int) float64 {
	return float64(scaleMinutePrice(raw, int64(divisor))) / 1000
}

// minuteAvg converts a raw minute average-price field to yuan.
func minuteAvg(raw int64, divisor int) float64 {
	return float64(raw) / (10000 * float64(divisor))
}

// scaleMinutePrice replicates eltdx checked_scale: raw*10/divisor, floored for
// negative remainders.
func scaleMinutePrice(raw, divisor int64) int64 {
	scaled := raw * 10
	q := scaled / divisor
	if scaled%divisor < 0 {
		q--
	}
	return q
}

// minutePriceDivisor returns the minute-bar price divisor for a 6-digit code.
// Minute responses carry no per-code precision metadata, so only the fund/ETF
// prefix rule applies: those codes already store prices in milli-yuan.
func minutePriceDivisor(code6 string) int {
	switch code6[:2] {
	case "15", "16", "50", "51", "52", "53", "56", "58":
		return 10
	}
	return 1
}

// readVarintSafe reads one signed varint at *off, advancing it. It reports false
// when the varint is unterminated or would run past the end of body.
func readVarintSafe(body []byte, off *int) (int64, bool) {
	start := *off
	for *off < len(body) {
		if body[*off]&0x80 == 0 {
			v, _ := codec.ReadVarint(body[start : *off+1])
			*off++
			return v, true
		}
		*off++
	}
	return 0, false
}

// parseDateYMD parses "yyyymmdd" or "yyyy-mm-dd" into a yyyymmdd u32.
func parseDateYMD(s string) (uint32, error) {
	y, m, d, err := parseDateParts(s)
	if err != nil {
		return 0, err
	}
	return uint32(y*10000 + m*100 + d), nil
}

// parseDateParts parses "yyyymmdd" or "yyyy-mm-dd" into year, month, day.
func parseDateParts(s string) (int, int, int, error) {
	var digits [8]byte
	j := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			continue
		}
		if s[i] < '0' || s[i] > '9' || j >= 8 {
			return 0, 0, 0, fmt.Errorf("go2tdx: invalid date %q", s)
		}
		digits[j] = s[i] - '0'
		j++
	}
	if j != 8 {
		return 0, 0, 0, fmt.Errorf("go2tdx: invalid date %q", s)
	}
	y := int(digits[0])*1000 + int(digits[1])*100 + int(digits[2])*10 + int(digits[3])
	m := int(digits[4])*10 + int(digits[5])
	d := int(digits[6])*10 + int(digits[7])
	if y < 1 || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, 0, fmt.Errorf("go2tdx: invalid date %q", s)
	}
	return y, m, d, nil
}

// pythonDateOrdinal returns Python's date.toordinal(): days since 0001-01-01,
// with 0001-01-01 == 1.
func pythonDateOrdinal(y, m, d int) int {
	py := y - 1
	daysBeforeMonth := [12]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	o := py*365 + py/4 - py/100 + py/400 + daysBeforeMonth[m-1] + d
	if m > 2 && (y%4 == 0 && (y%100 != 0 || y%400 == 0)) {
		o++
	}
	return o
}
