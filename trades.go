package go2tdx

import (
	"encoding/binary"
	"time"
)

const (
	msgTodayTicks      uint16 = 0x0FC5 // 4037
	msgHistoricalTicks uint16 = 0x0FC6 // 4038
)

// Today returns the current-day trade ticks for a security.
// Request (12B): market u16 | code 6B | start u16 | count u16 (1..1800).
func (a *TradesAPI) Today(code string, start, count uint16) ([]Tick, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], market)
	copy(data[2:8], code6)
	binary.LittleEndian.PutUint16(data[8:10], start)
	binary.LittleEndian.PutUint16(data[10:12], count)
	body, err := a.c.pool.Exec(msgTodayTicks, data)
	if err != nil {
		return nil, err
	}
	return decodeTodayTicks(body)
}

// History returns the trade ticks for a past trading date.
// date is "yyyymmdd" (or "yyyy-mm-dd").
// Request (16B): trading_date u32 | market u16 | code 6B | start u16 | count u16.
func (a *TradesAPI) History(code, date string, start, count uint16) ([]Tick, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	ymd, err := parseDateYMD(date)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], ymd)
	binary.LittleEndian.PutUint16(data[4:6], market)
	copy(data[6:12], code6)
	binary.LittleEndian.PutUint16(data[12:14], start)
	binary.LittleEndian.PutUint16(data[14:16], count)
	body, err := a.c.pool.Exec(msgHistoricalTicks, data)
	if err != nil {
		return nil, err
	}
	ticks, _, err := decodeHistoricalTicks(body, ymd)
	return ticks, err
}

// decodeTodayTicks decodes a today_ticks (0x0FC5) response body:
// 2B count u16 | N records (time_minutes u16 + 5 varints: price_delta,
// volume, order_count, status, tail). price_delta accumulates across records
// onto a running price; Tick.Price = acc / 100.
func decodeTodayTicks(body []byte) ([]Tick, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	ticks, err := decodeTickRecords(body, 2, 0)
	return ticks, err
}

// decodeHistoricalTicks decodes a historical_ticks (0x0FC6) response body:
// 2B count u16 | 4B price_base f32 | N records (same shape as today_ticks;
// the last varint is reserved_zero). price_base is informational: it is
// returned but not added to the price accumulation.
func decodeHistoricalTicks(body []byte, ymd uint32) ([]Tick, float64, error) {
	if len(body) < 6 {
		return nil, 0, errShortBody
	}
	priceBase := f32At(body, 2)
	ticks, err := decodeTickRecords(body, 6, ymd)
	return ticks, priceBase, err
}

// decodeTickRecords decodes N tick records starting at off. Each record is a
// time_minutes u16 followed by 5 varints (price_delta, volume, order_count,
// status, tail/reserved_zero). A truncated trailing record is dropped.
func decodeTickRecords(body []byte, off int, ymd uint32) ([]Tick, error) {
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	ticks := make([]Tick, 0, n)
	var priceAcc int64
	for i := 0; i < n; i++ {
		if off+2 > len(body) {
			break
		}
		minutes := binary.LittleEndian.Uint16(body[off : off+2])
		off += 2
		delta, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		vol, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		orderCount, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		status, ok := readVarintSafe(body, &off)
		if !ok {
			break
		}
		if _, ok := readVarintSafe(body, &off); !ok { // tail / reserved_zero
			break
		}
		priceAcc += delta
		ticks = append(ticks, Tick{
			Time:       tickTime(ymd, minutes),
			Price:      float64(priceAcc) / 100.0,
			Volume:     int(vol),
			OrderCount: int(orderCount),
			Status:     int(status),
		})
	}
	return ticks, nil
}

// tickTime builds the Tick timestamp from a yyyymmdd date and a minute-of-day
// offset. A zero ymd (today_ticks) yields the zero date 0001-01-01.
func tickTime(ymd uint32, minutes uint16) time.Time {
	y, m, d := 1, 1, 1
	if ymd != 0 {
		y = int(ymd / 10000)
		m = int((ymd / 100) % 100)
		d = int(ymd % 100)
	}
	return time.Date(y, time.Month(m), d, int(minutes/60), int(minutes%60), 0, 0, time.UTC)
}
