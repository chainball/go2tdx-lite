package go2tdx

import (
	"encoding/binary"
	"time"
)

const msgAuctionSeries uint16 = 0x056A // 1386

// auctionSeriesMode is the wire selector the 0x056A request carries; eltdx
// always sends 3.
const auctionSeriesMode uint32 = 3

// auctionSeriesLimit is the page size the 0x056A request carries; eltdx always
// sends 500.
const auctionSeriesLimit uint32 = 500

// Series returns the call-auction points of a security's opening auction.
// code accepts the usual forms ("sz000988", "000988"); date is "yyyymmdd" or
// "yyyy-mm-dd", and an empty date means today (wire trading_date 0).
// Request (28B): market u8 | 0 | code 6B | trading_date u32 | mode u32 (3) |
// 4B reserved | start u32 | limit u32 (500).
func (a *AuctionsAPI) Series(code, date string) ([]Auction, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	var ymd uint32
	if date != "" {
		if ymd, err = parseDateYMD(date); err != nil {
			return nil, err
		}
	}
	data := make([]byte, 28)
	data[0] = byte(market)
	copy(data[2:8], code6)
	binary.LittleEndian.PutUint32(data[8:12], ymd)
	binary.LittleEndian.PutUint32(data[12:16], auctionSeriesMode)
	binary.LittleEndian.PutUint32(data[20:24], 0) // start
	binary.LittleEndian.PutUint32(data[24:28], auctionSeriesLimit)
	body, err := a.c.pool.Exec(msgAuctionSeries, data)
	if err != nil {
		return nil, err
	}
	return decodeAuctions(body, ymd)
}

// decodeAuctions decodes an auction_series (0x056A) response body:
// 2B count u16 | N×16B records (minute_of_day u16 | price f32 |
// matched_volume u32 | unmatched_signed i32 | reserved 0x0E | second u8).
// ymd is the requested trading date; 0 (today) yields the zero date 0001-01-01.
// A truncated trailing record is dropped.
func decodeAuctions(body []byte, ymd uint32) ([]Auction, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	points := make([]Auction, 0, n)
	for i := 0; i < n; i++ {
		off := 2 + i*16
		if off+16 > len(body) {
			break
		}
		minutes := binary.LittleEndian.Uint16(body[off : off+2])
		second := int(body[off+15])
		points = append(points, Auction{
			Time:          auctionTime(ymd, minutes, second),
			Price:         f32At(body, off+2),
			MatchedVolume: binary.LittleEndian.Uint32(body[off+6 : off+10]),
			Unmatched:     int32(binary.LittleEndian.Uint32(body[off+10 : off+14])),
		})
	}
	return points, nil
}

// auctionTime builds the Auction timestamp from a yyyymmdd date, a
// minute-of-day offset and a second. A zero ymd yields the zero date.
func auctionTime(ymd uint32, minutes uint16, second int) time.Time {
	y, m, d := 1, 1, 1
	if ymd != 0 {
		y = int(ymd / 10000)
		m = int((ymd / 100) % 100)
		d = int(ymd % 100)
	}
	return time.Date(y, time.Month(m), d, int(minutes/60), int(minutes%60), second, 0, time.UTC)
}
