package go2tdx

import (
	"encoding/binary"
	"math"

	"github.com/chainball/go2tdx-lite/internal/gbk"
)

const (
	msgSecurityList  uint16 = 1101
	msgSecurityCount uint16 = 1102
)

// List returns one page of the security table for a market.
// Request (14B): market u16 | start u32 | limit u32 | 4x0.
func (a *CodesAPI) List(market uint16, start, limit uint32) ([]Security, error) {
	data := make([]byte, 14)
	binary.LittleEndian.PutUint16(data[0:2], market)
	binary.LittleEndian.PutUint32(data[2:6], start)
	binary.LittleEndian.PutUint32(data[6:10], limit)
	body, err := a.c.pool.Exec(msgSecurityList, data)
	if err != nil {
		return nil, err
	}
	return decodeSecurityList(body)
}

// Count returns the number of securities in a market.
// Request (6B): market u16 | client_date u32 (yyyymmdd, today).
func (a *CodesAPI) Count(market uint16) (uint16, error) {
	data := make([]byte, 6)
	binary.LittleEndian.PutUint16(data[0:2], market)
	binary.LittleEndian.PutUint32(data[2:6], todayYMD())
	body, err := a.c.pool.Exec(msgSecurityCount, data)
	if err != nil {
		return 0, err
	}
	return decodeSecurityCount(body)
}

// decodeSecurityList reads 2B count u16 + N x 37B records:
// code 6B ASCII | multiple u16 | name 16B GBK NUL-pad | volume_ratio_base f32 |
// decimal u8 | previous_close f32 | unknown3 4B.
// A trailing partial record is dropped.
func decodeSecurityList(body []byte) ([]Security, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	out := make([]Security, 0, n)
	off := 2
	for i := 0; i < n; i++ {
		if off+37 > len(body) {
			break
		}
		rec := body[off : off+37]
		off += 37
		name, _ := gbk.Decode(trimNUL(rec[8:24]))
		out = append(out, Security{
			Code:     string(rec[0:6]),
			Name:     name,
			Multiple: int(binary.LittleEndian.Uint16(rec[6:8])),
			Decimal:  int(rec[28]),
			PreClose: float64(math.Float32frombits(binary.LittleEndian.Uint32(rec[29:33]))),
		})
	}
	return out, nil
}

// decodeSecurityCount reads 2B count u16.
func decodeSecurityCount(body []byte) (uint16, error) {
	if len(body) < 2 {
		return 0, errShortBody
	}
	return binary.LittleEndian.Uint16(body[0:2]), nil
}
