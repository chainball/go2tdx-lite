package go2tdx

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	msgCapitalChanges uint16 = 15
	msgFinanceBatch   uint16 = 16
)

// maxCorporateCodes is the protocol cap on per-request security codes.
const maxCorporateCodes = 200

// CapitalChanges returns 除权除息 / 股本变动 records for the given codes.
// Request (2B count u16 + N×7B market_id u8 + code 6B); response is a block list.
func (a *CorporateAPI) CapitalChanges(codes []string) ([]XdxrRecord, error) {
	data, err := encodeCorporateRequest(codes)
	if err != nil {
		return nil, err
	}
	body, err := a.c.pool.Exec(msgCapitalChanges, data)
	if err != nil {
		return nil, err
	}
	return decodeCapitalChanges(body)
}

// FinanceBatch returns finance summaries for the given codes.
// Request is identical to capital_changes; response is N×143B records.
func (a *CorporateAPI) FinanceBatch(codes []string) ([]FinanceInfo, error) {
	data, err := encodeCorporateRequest(codes)
	if err != nil {
		return nil, err
	}
	body, err := a.c.pool.Exec(msgFinanceBatch, data)
	if err != nil {
		return nil, err
	}
	return decodeFinanceBatch(body)
}

// encodeCorporateRequest builds the shared capital_changes/finance_batch request:
// 2B count u16 + N×7B (market_id u8 | code 6B).
func encodeCorporateRequest(codes []string) ([]byte, error) {
	if len(codes) > maxCorporateCodes {
		return nil, errors.New("go2tdx: too many codes (max 200)")
	}
	data := make([]byte, 2+len(codes)*7)
	binary.LittleEndian.PutUint16(data[0:2], uint16(len(codes)))
	for i, c := range codes {
		market, code6, err := NormalizeCode(c)
		if err != nil {
			return nil, err
		}
		off := 2 + i*7
		data[off] = byte(market)
		copy(data[off+1:off+7], code6)
	}
	return data, nil
}

// decodeCapitalChanges reads a capital_changes response body: 2B block_count u16
// + block loop of 9B header (market_id u8 | code 6B | record_count u16) +
// N×29B records (market_id u8 | code 6B | reserved_7 u8 | date u32 yyyymmdd |
// category u8 | c1..c4 f32). A trailing partial header/record is dropped.
func decodeCapitalChanges(body []byte) ([]XdxrRecord, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	out := make([]XdxrRecord, 0, n)
	off := 2
	for i := 0; i < n; i++ {
		if off+9 > len(body) {
			break
		}
		recordCount := int(binary.LittleEndian.Uint16(body[off+7 : off+9]))
		off += 9
		for j := 0; j < recordCount; j++ {
			if off+29 > len(body) {
				return out, nil
			}
			rec := body[off : off+29]
			off += 29
			category := int(rec[12])
			c1 := f32At(rec, 13)
			c2 := f32At(rec, 17)
			c3 := f32At(rec, 21)
			c4 := f32At(rec, 25)
			scaleCValues(category, &c1, &c2, &c3, &c4)
			out = append(out, XdxrRecord{
				Market:   uint16(rec[0]),
				Code:     string(rec[1:7]),
				Date:     padDate(binary.LittleEndian.Uint32(rec[8:12])),
				Category: category,
				C1:       c1, C2: c2, C3: c3, C4: c4,
			})
		}
	}
	return out, nil
}

// decodeFinanceBatch reads a finance_batch response body: 2B count u16 + N×143B
// records (market_id u8 | code 6B | finance_info 136B). finance_info: liu_tong_gu_ben
// f32 | province u16 | industry u16 | updated_date u32 | ipo_date u32 | 30×f32.
func decodeFinanceBatch(body []byte) ([]FinanceInfo, error) {
	if len(body) < 2 {
		return nil, errShortBody
	}
	n := int(binary.LittleEndian.Uint16(body[0:2]))
	out := make([]FinanceInfo, 0, n)
	off := 2
	for i := 0; i < n; i++ {
		if off+143 > len(body) {
			break
		}
		rec := body[off : off+143]
		off += 143
		fin := rec[7:]
		f := FinanceInfo{
			Market:       uint16(rec[0]),
			Code:         string(rec[1:7]),
			LiuTongGuBen: f32At(fin, 0),
			Province:     binary.LittleEndian.Uint16(fin[4:6]),
			Industry:     binary.LittleEndian.Uint16(fin[6:8]),
			UpdatedDate:  padDate(binary.LittleEndian.Uint32(fin[8:12])),
			IPODate:      padDate(binary.LittleEndian.Uint32(fin[12:16])),
		}
		f.ZongGuBen = f32At(fin, 16)
		f.GuoJiaGu = f32At(fin, 20)
		f.FaQiRenFaRenGu = f32At(fin, 24)
		f.FaRenGu = f32At(fin, 28)
		f.BGu = f32At(fin, 32)
		f.HGu = f32At(fin, 36)
		f.EPS = f32At(fin, 40)
		f.ZongZiChan = f32At(fin, 44)
		f.LiuDongZiChan = f32At(fin, 48)
		f.GuDingZiChan = f32At(fin, 52)
		f.WuXingZiChan = f32At(fin, 56)
		f.GuDongRenShu = f32At(fin, 60)
		f.LiuDongFuZhai = f32At(fin, 64)
		f.ChangQiFuZhai = f32At(fin, 68)
		f.ZiBenGongJiJin = f32At(fin, 72)
		f.JingZiChan = f32At(fin, 76)
		f.ZhuYingShouRu = f32At(fin, 80)
		f.ZhuYingLiRun = f32At(fin, 84)
		f.YingShouZhangKuan = f32At(fin, 88)
		f.YingYeLiRun = f32At(fin, 92)
		f.TouZiShouYu = f32At(fin, 96)
		f.JingYingXianJinLiu = f32At(fin, 100)
		f.ZongXianJinLiu = f32At(fin, 104)
		f.CunHuo = f32At(fin, 108)
		f.LiRunZongHe = f32At(fin, 112)
		f.ShuiHouLiRun = f32At(fin, 116)
		f.JingLiRun = f32At(fin, 120)
		f.WeiFenLiRun = f32At(fin, 124)
		f.MeiGuJingZiChan = f32At(fin, 128)
		f.BaoLiu2 = f32At(fin, 132)
		out = append(out, f)
	}
	return out, nil
}

// scaleCValues applies the capital_changes per-category unit rule: share-count
// categories (2,3,5,7,8,9,10) scale all four c-values ×10000; category 6 scales
// only c3 ×10000; all other categories keep raw values.
func scaleCValues(category int, c1, c2, c3, c4 *float64) {
	switch category {
	case 2, 3, 5, 7, 8, 9, 10:
		*c1 *= 10000
		*c2 *= 10000
		*c3 *= 10000
		*c4 *= 10000
	case 6:
		*c3 *= 10000
	}
}

// f32At reads a little-endian f32 at off and widens it to float64.
func f32At(b []byte, off int) float64 {
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4])))
}
