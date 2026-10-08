package go2tdx

import (
	"encoding/binary"
	"time"

	"github.com/chainball/go2tdx-lite/internal/codec"
)

const msgKlines uint16 = 1325

func (a *BarsAPI) Get(code string, period Period, start, count uint16) ([]Bar, error) {
	return a.get(code, period, start, count, AdjustNone, false)
}

func (a *BarsAPI) GetIndex(code string, period Period, start, count uint16) ([]Bar, error) {
	return a.get(code, period, start, count, AdjustNone, true)
}

func (a *BarsAPI) get(code string, period Period, start, count uint16, adjust uint16, index bool) ([]Bar, error) {
	market, code6, err := NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	// Classic 0x052D request data is 26 bytes: market u16 | code 6B |
	// period.raw u16 | period.param u16 | start u16 | count u16 | 10 zero
	// bytes. It carries no adjust/anchor field — the gotdx-era servers reject
	// the eltdx 42-byte form (adjust u16 @16 + anchor u32 @18 + 20 zeros), so
	// the adjust param is intentionally not sent.
	data := make([]byte, 26)
	binary.LittleEndian.PutUint16(data[0:2], market)
	copy(data[2:8], code6)
	binary.LittleEndian.PutUint16(data[8:10], period.Raw)
	binary.LittleEndian.PutUint16(data[10:12], period.Param)
	binary.LittleEndian.PutUint16(data[12:14], start)
	binary.LittleEndian.PutUint16(data[14:16], count)
	body, err := a.c.pool.Exec(msgKlines, data)
	if err != nil {
		return nil, err
	}
	return decodeKlines(body, period.Raw, index)
}

// decodeKlines decodes a klines (0x052D) response body: 2B count u16 + N
// variable records (see docs/COMMANDS_7709.md §3 bars).
func decodeKlines(body []byte, raw uint16, index bool) ([]Bar, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	off := 2
	bars := make([]Bar, 0, n)
	lastClose := float64(0) // milli
	for i := 0; i < n; i++ {
		need := 16
		if index {
			need = 20
		}
		if off+need > len(body) {
			break
		}
		t := decodeKlineTime(raw, body[off:off+4])
		off += 4
		openDelta, m := codec.ReadVarint(body[off:])
		off += m
		closeDelta, m := codec.ReadVarint(body[off:])
		off += m
		highDelta, m := codec.ReadVarint(body[off:])
		off += m
		lowDelta, m := codec.ReadVarint(body[off:])
		off += m
		vol := codec.DecodeVolume(binary.LittleEndian.Uint32(body[off : off+4]))
		amt := codec.DecodeVolume(binary.LittleEndian.Uint32(body[off+4 : off+8]))
		off += 8

		// open = last_close + open_delta; close/high/low 相对 open。
		openMilli := lastClose + float64(openDelta)
		closeMilli := openMilli + float64(closeDelta)
		highMilli := openMilli + float64(highDelta)
		lowMilli := openMilli + float64(lowDelta)
		lastClose = closeMilli

		bar := Bar{
			Time:   t,
			Open:   openMilli / 1000,
			Close:  closeMilli / 1000,
			High:   highMilli / 1000,
			Low:    lowMilli / 1000,
			Volume: vol / 100,
			Amount: amt,
		}
		if index {
			bar.UpCount = int(binary.LittleEndian.Uint16(body[off : off+2]))
			bar.DownCount = int(binary.LittleEndian.Uint16(body[off+2 : off+4]))
			off += 4
		}
		bars = append(bars, bar)
	}
	return bars, nil
}

// decodeKlineTime decodes the 4-byte time field of a kline record.
func decodeKlineTime(raw uint16, b []byte) time.Time {
	switch raw {
	case 0, 1, 2, 3, 7, 8:
		// packed date u16 LE: year=(x>>11)+2004, month=(x%2048)/100, day=(x%2048)%100
		x := int(binary.LittleEndian.Uint16(b[0:2]))
		year := (x >> 11) + 2004
		month := (x % 2048) / 100
		day := (x % 2048) % 100
		minute := int(binary.LittleEndian.Uint16(b[2:4])) // minute-of-day
		return time.Date(year, time.Month(month), day, minute/60, minute%60, 0, 0, time.FixedZone("CST", 8*3600))
	case 13:
		secs := binary.LittleEndian.Uint32(b[0:4]) // seconds since 2003-12-31 00:00:00 Shanghai
		return time.Date(2003, 12, 31, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)).Add(time.Duration(secs) * time.Second)
	default: // 4,5,6,9,10,11
		d := binary.LittleEndian.Uint32(b[0:4]) // yyyymmdd
		year := int(d / 10000)
		month := int((d / 100) % 100)
		day := int(d % 100)
		return time.Date(year, time.Month(month), day, 15, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
}
