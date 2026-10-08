package go2tdx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"

	"github.com/chainball/go2tdx-lite/internal/codec"
)

const (
	msgLegacyQuotes   uint16 = 0x053E // 1342
	msgRefreshStream  uint16 = 0x0547 // 1351
	msgCategoryQuotes uint16 = 0x054B // 1355
	msgSnapshots      uint16 = 0x054C // 1356
)

// quoteListPrefix is the fixed 8-byte prefix for snapshots/legacy_quotes requests.
var quoteListPrefix = [8]byte{5, 0, 0, 0, 0, 0, 0, 0}

// refreshXorKey is the byte the refresh_stream payload is XOR-obfuscated with.
const refreshXorKey byte = 0x93

// quoteCode is a normalized (market, 6-digit code) pair.
type quoteCode struct {
	market uint16
	code   string
}

// normalizeQuoteCodes normalizes each input code into a (market, 6-digit code)
// pair, preserving order.
func normalizeQuoteCodes(codes []string) ([]quoteCode, error) {
	out := make([]quoteCode, len(codes))
	for i, c := range codes {
		m, c6, err := NormalizeCode(c)
		if err != nil {
			return nil, err
		}
		out[i] = quoteCode{market: m, code: c6}
	}
	return out, nil
}

// marker returns the 7-byte market+code marker that begins each variable record.
func (c quoteCode) marker() []byte {
	m := make([]byte, 7)
	m[0] = byte(c.market)
	copy(m[1:], c.code)
	return m
}

// divisor returns the price divisor for a code (1 standard, 10 ETF).
func (c quoteCode) divisor() int64 {
	return int64(PriceDivisor(c.code, 2))
}

// encodeQuoteCodeList builds the snapshots/legacy_quotes request data:
// 8B fixed prefix | count u16 | N×(market u8 + code 6B).
func encodeQuoteCodeList(codes []quoteCode) []byte {
	data := make([]byte, 10+len(codes)*7)
	copy(data[:8], quoteListPrefix[:])
	binary.LittleEndian.PutUint16(data[8:10], uint16(len(codes)))
	off := 10
	for _, c := range codes {
		data[off] = byte(c.market)
		copy(data[off+1:off+7], c.code)
		off += 7
	}
	return data
}

// encodeRefreshList builds the refresh_stream request data:
// count u16 | N×(market u8 + code 6B + cursor u32).
func encodeRefreshList(codes []quoteCode) []byte {
	data := make([]byte, 2+len(codes)*11)
	binary.LittleEndian.PutUint16(data[0:2], uint16(len(codes)))
	off := 2
	for _, c := range codes {
		data[off] = byte(c.market)
		copy(data[off+1:off+7], c.code)
		binary.LittleEndian.PutUint32(data[off+7:off+11], 0) // cursor
		off += 11
	}
	return data
}

// encodeCategoryRequest builds the 18B category_quotes request data:
// category | sort_type | start | count | sort_reverse | 5 | filter | 1 | 0.
func encodeCategoryRequest(category, sortType, start, count uint16) []byte {
	sortReverse := uint16(1) // default descending
	if sortType == 0 {
		sortReverse = 0
	}
	data := make([]byte, 18)
	binary.LittleEndian.PutUint16(data[0:2], category)
	binary.LittleEndian.PutUint16(data[2:4], sortType)
	binary.LittleEndian.PutUint16(data[4:6], start)
	binary.LittleEndian.PutUint16(data[6:8], count)
	binary.LittleEndian.PutUint16(data[8:10], sortReverse)
	binary.LittleEndian.PutUint16(data[10:12], 5)
	binary.LittleEndian.PutUint16(data[12:14], 0) // filter_raw
	binary.LittleEndian.PutUint16(data[14:16], 1)
	binary.LittleEndian.PutUint16(data[16:18], 0)
	return data
}

// Snapshots returns real-time quote snapshots for the given codes (0x054C).
func (q *QuotesAPI) Snapshots(codes []string) ([]Quote, error) {
	norm, err := normalizeQuoteCodes(codes)
	if err != nil {
		return nil, err
	}
	body, err := q.c.pool.Exec(msgSnapshots, encodeQuoteCodeList(norm))
	if err != nil {
		return nil, err
	}
	return decodeSnapshots(body, norm)
}

// Legacy returns legacy real-time quotes for the given codes (0x053E).
func (q *QuotesAPI) Legacy(codes []string) ([]Quote, error) {
	if len(codes) == 0 {
		return nil, errors.New("go2tdx: legacy_quotes requires at least one code")
	}
	norm, err := normalizeQuoteCodes(codes)
	if err != nil {
		return nil, err
	}
	body, err := q.c.pool.Exec(msgLegacyQuotes, encodeQuoteCodeList(norm))
	if err != nil {
		return nil, err
	}
	return decodeLegacy(body, norm)
}

// Refresh returns refresh-stream quotes for the given codes (0x0547).
func (q *QuotesAPI) Refresh(codes []string) ([]Quote, error) {
	norm, err := normalizeQuoteCodes(codes)
	if err != nil {
		return nil, err
	}
	body, err := q.c.pool.Exec(msgRefreshStream, encodeRefreshList(norm))
	if err != nil {
		return nil, err
	}
	return decodeRefresh(body, norm)
}

// Category returns a page of quotes for a market category (0x054B).
func (q *QuotesAPI) Category(category, sortType uint16, start, count uint16) ([]Quote, error) {
	body, err := q.c.pool.Exec(msgCategoryQuotes, encodeCategoryRequest(category, sortType, start, count))
	if err != nil {
		return nil, err
	}
	return decodeCategory(body)
}

// quotePrice converts a decode_k/category raw price (already a delta-summed
// varint) to yuan: floor(raw*10/divisor)/1000.
func quotePrice(raw int64, divisor int64) float64 {
	return math.Floor(float64(raw*10)/float64(divisor)) / 1000.0
}

// legacyPrice converts a legacy_quotes raw price to yuan: raw/(100*divisor).
// legacy close/diffs are already in cent-units (no ×10, no floor).
func legacyPrice(raw int64, divisor int64) float64 {
	return float64(raw) / (100.0 * float64(divisor))
}

// readK reads the 5 price varints (current + 4 deltas) and returns the summed
// raw values, advancing off. Reports false on truncation.
func readK(body []byte, off *int) (cur, lastClose, open, high, low int64, ok bool) {
	cur, ok = readVarintSafe(body, off)
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	var d [4]int64
	for i := range d {
		d[i], ok = readVarintSafe(body, off)
		if !ok {
			return 0, 0, 0, 0, 0, false
		}
	}
	return cur, cur + d[0], cur + d[1], cur + d[2], cur + d[3], true
}

// splitByMarkers splits a variable-record payload into per-record slices using
// the 7-byte market+code markers of the requested codes, in order. Each record
// spans [starts[i], starts[i+1]) (or to end for the last).
func splitByMarkers(data []byte, codes []quoteCode) [][]byte {
	if len(codes) == 0 {
		return nil
	}
	starts := make([]int, 0, len(codes))
	searchFrom := 0
	for _, c := range codes {
		marker := c.marker()
		rel := bytes.Index(data[searchFrom:], marker)
		if rel < 0 {
			break
		}
		pos := searchFrom + rel
		starts = append(starts, pos)
		searchFrom = pos + len(marker)
	}
	records := make([][]byte, 0, len(starts))
	for i, s := range starts {
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		records = append(records, data[s:end])
	}
	return records
}

// quoteHead reads the common record header (market, code, active1) and returns
// the code's divisor.
func quoteHead(rec []byte, off int) (q Quote, div int64, next int, ok bool) {
	if off+9 > len(rec) {
		return Quote{}, 0, off, false
	}
	code := string(rec[off+1 : off+7])
	return Quote{
		Market:  uint16(rec[off]),
		Code:    code,
		Active1: binary.LittleEndian.Uint16(rec[off+7 : off+9]),
	}, int64(PriceDivisor(code, 2)), off + 9, true
}

// decodeSnapshots decodes a snapshots (0x054C) response body:
// 2B reserved | 2B count | N variable records split by requested-code markers.
func decodeSnapshots(body []byte, codes []quoteCode) ([]Quote, error) {
	if len(body) < 4 {
		return nil, errShortBody
	}
	count := int(binary.LittleEndian.Uint16(body[2:4]))
	if count > len(codes) {
		count = len(codes)
	}
	quotes := make([]Quote, 0, count)
	for _, rec := range splitByMarkers(body[4:], codes[:count]) {
		if q, ok := decodeSnapshotRecord(rec); ok {
			quotes = append(quotes, q)
		}
	}
	return quotes, nil
}

func decodeSnapshotRecord(rec []byte) (Quote, bool) {
	q, div, off, ok := quoteHead(rec, 0)
	if !ok {
		return Quote{}, false
	}
	cur, lastClose, open, high, low, ok := readK(rec, &off)
	if !ok {
		return Quote{}, false
	}
	q.Last = quotePrice(cur, div)
	q.PreClose = quotePrice(lastClose, div)
	q.Open = quotePrice(open, div)
	q.High = quotePrice(high, div)
	q.Low = quotePrice(low, div)
	if _, ok = readVarintSafe(rec, &off); !ok { // time
		return Quote{}, false
	}
	if _, ok = readVarintSafe(rec, &off); !ok { // unknown_after_time
		return Quote{}, false
	}
	if q.TotalHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if q.CurrentHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if off+4 > len(rec) {
		return Quote{}, false
	}
	q.Amount = codec.DecodeVolume(binary.LittleEndian.Uint32(rec[off : off+4]))
	off += 4
	var inside, outer, openAmt int64
	if inside, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if outer, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if _, ok = readVarintSafe(rec, &off); !ok { // unknown_after_outer
		return Quote{}, false
	}
	if openAmt, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	q.InsideDish = float64(inside)
	q.OuterDisc = float64(outer)
	q.OpenAmount = float64(openAmt)
	// single bid/ask level
	bidDelta, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	askDelta, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	bidVol, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	askVol, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	q.Bids[0] = QuoteLevel{Price: quotePrice(cur+bidDelta, div), Volume: float64(bidVol)}
	q.Asks[0] = QuoteLevel{Price: quotePrice(cur+askDelta, div), Volume: float64(askVol)}
	return q, true
}

// decodeLegacy decodes a legacy_quotes (0x053E) response body:
// 2B reserved | 2B count | N variable records split by requested-code markers.
func decodeLegacy(body []byte, codes []quoteCode) ([]Quote, error) {
	if len(body) < 4 {
		return nil, errShortBody
	}
	count := int(binary.LittleEndian.Uint16(body[2:4]))
	if count > len(codes) {
		count = len(codes)
	}
	quotes := make([]Quote, 0, count)
	for _, rec := range splitByMarkers(body[4:], codes[:count]) {
		if q, ok := decodeLegacyRecord(rec); ok {
			quotes = append(quotes, q)
		}
	}
	return quotes, nil
}

func decodeLegacyRecord(rec []byte) (Quote, bool) {
	q, div, off, ok := quoteHead(rec, 0)
	if !ok {
		return Quote{}, false
	}
	closeRaw, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	preCloseDiff, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	openDiff, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	highDiff, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	lowDiff, ok := readVarintSafe(rec, &off)
	if !ok {
		return Quote{}, false
	}
	q.Last = legacyPrice(closeRaw, div)
	q.PreClose = legacyPrice(closeRaw+preCloseDiff, div)
	q.Open = legacyPrice(closeRaw+openDiff, div)
	q.High = legacyPrice(closeRaw+highDiff, div)
	q.Low = legacyPrice(closeRaw+lowDiff, div)
	if _, ok = readVarintSafe(rec, &off); !ok { // server_time
		return Quote{}, false
	}
	if _, ok = readVarintSafe(rec, &off); !ok { // unknown_after_time
		return Quote{}, false
	}
	if q.TotalHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if q.CurrentHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if off+4 > len(rec) {
		return Quote{}, false
	}
	q.Amount = codec.DecodeVolume(binary.LittleEndian.Uint32(rec[off : off+4]))
	off += 4
	var inside, outer, openAmt int64
	if inside, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if outer, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if _, ok = readVarintSafe(rec, &off); !ok { // unknown_after_outer
		return Quote{}, false
	}
	if openAmt, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	q.InsideDish = float64(inside)
	q.OuterDisc = float64(outer)
	q.OpenAmount = float64(openAmt)
	for i := 0; i < 5; i++ {
		bidDelta, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		askDelta, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		bidVol, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		askVol, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		q.Bids[i] = QuoteLevel{Price: legacyPrice(closeRaw+bidDelta, div), Volume: float64(bidVol)}
		q.Asks[i] = QuoteLevel{Price: legacyPrice(closeRaw+askDelta, div), Volume: float64(askVol)}
	}
	// trading_status u16 + 4 tail varints + optional 4B tail are trailing.
	if off+2 <= len(rec) {
		q.Status = int64(binary.LittleEndian.Uint16(rec[off : off+2]))
	}
	return q, true
}

// decodeRefresh XOR-decodes and decodes a refresh_stream (0x0547) response:
// raw payload is XOR 0x93 obfuscated; decoded is 2B count | N variable records.
func decodeRefresh(body []byte, codes []quoteCode) ([]Quote, error) {
	decoded := make([]byte, len(body))
	for i, b := range body {
		decoded[i] = b ^ refreshXorKey
	}
	if len(decoded) < 2 {
		return nil, errShortBody
	}
	count := int(binary.LittleEndian.Uint16(decoded[0:2]))
	if count == 0 {
		return []Quote{}, nil
	}
	if count > len(codes) {
		count = len(codes)
	}
	quotes := make([]Quote, 0, count)
	for _, rec := range splitByMarkers(decoded[2:], codes[:count]) {
		if q, ok := decodeRefreshRecord(rec); ok {
			quotes = append(quotes, q)
		}
	}
	return quotes, nil
}

func decodeRefreshRecord(rec []byte) (Quote, bool) {
	q, div, off, ok := quoteHead(rec, 0)
	if !ok {
		return Quote{}, false
	}
	cur, lastClose, open, high, low, ok := readK(rec, &off)
	if !ok {
		return Quote{}, false
	}
	q.Last = quotePrice(cur, div)
	q.PreClose = quotePrice(lastClose, div)
	q.Open = quotePrice(open, div)
	q.High = quotePrice(high, div)
	q.Low = quotePrice(low, div)
	if off+4 > len(rec) { // update_time u32
		return Quote{}, false
	}
	off += 4
	if q.Status, ok = readVarintSafe(rec, &off); !ok { // status
		return Quote{}, false
	}
	if q.TotalHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if q.CurrentHand, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if off+4 > len(rec) {
		return Quote{}, false
	}
	q.Amount = codec.DecodeVolume(binary.LittleEndian.Uint32(rec[off : off+4]))
	off += 4
	var inside, outer, openAmt int64
	if inside, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if outer, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	if _, ok = readVarintSafe(rec, &off); !ok { // unknown_after_outer
		return Quote{}, false
	}
	if openAmt, ok = readVarintSafe(rec, &off); !ok {
		return Quote{}, false
	}
	q.InsideDish = float64(inside)
	q.OuterDisc = float64(outer)
	q.OpenAmount = float64(openAmt)
	for i := 0; i < 5; i++ {
		buyDelta, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		sellDelta, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		buyVol, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		sellVol, ok := readVarintSafe(rec, &off)
		if !ok {
			return Quote{}, false
		}
		q.Bids[i] = QuoteLevel{Price: quotePrice(cur+buyDelta, div), Volume: float64(buyVol)}
		q.Asks[i] = QuoteLevel{Price: quotePrice(cur+sellDelta, div), Volume: float64(sellVol)}
	}
	return q, true
}

// decodeCategory decodes a category_quotes (0x054B) response body:
// 2B header | 2B count | N variable records ending in a 56B fixed tail.
func decodeCategory(body []byte) ([]Quote, error) {
	if len(body) < 4 {
		return nil, errShortBody
	}
	count := int(binary.LittleEndian.Uint16(body[2:4]))
	off := 4
	quotes := make([]Quote, 0, count)
	for i := 0; i < count; i++ {
		q, next, ok := decodeCategoryRecord(body, off)
		if !ok {
			break
		}
		quotes = append(quotes, q)
		off = next
	}
	return quotes, nil
}

func decodeCategoryRecord(body []byte, off int) (Quote, int, bool) {
	q, div, off, ok := quoteHead(body, off)
	if !ok {
		return Quote{}, off, false
	}
	closeRaw, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	preCloseDiff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	openDiff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	highDiff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	lowDiff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	q.Last = quotePrice(closeRaw, div)
	q.PreClose = quotePrice(closeRaw+preCloseDiff, div)
	q.Open = quotePrice(closeRaw+openDiff, div)
	q.High = quotePrice(closeRaw+highDiff, div)
	q.Low = quotePrice(closeRaw+lowDiff, div)
	if _, ok = readVarintSafe(body, &off); !ok { // server_time
		return Quote{}, off, false
	}
	if _, ok = readVarintSafe(body, &off); !ok { // neg_price
		return Quote{}, off, false
	}
	if q.TotalHand, ok = readVarintSafe(body, &off); !ok {
		return Quote{}, off, false
	}
	if q.CurrentHand, ok = readVarintSafe(body, &off); !ok {
		return Quote{}, off, false
	}
	if off+4 > len(body) {
		return Quote{}, off, false
	}
	q.Amount = codec.DecodeVolume(binary.LittleEndian.Uint32(body[off : off+4]))
	off += 4
	var inside, outer, openAmt int64
	if inside, ok = readVarintSafe(body, &off); !ok {
		return Quote{}, off, false
	}
	if outer, ok = readVarintSafe(body, &off); !ok {
		return Quote{}, off, false
	}
	if _, ok = readVarintSafe(body, &off); !ok { // after_outer
		return Quote{}, off, false
	}
	if openAmt, ok = readVarintSafe(body, &off); !ok {
		return Quote{}, off, false
	}
	q.InsideDish = float64(inside)
	q.OuterDisc = float64(outer)
	q.OpenAmount = float64(openAmt)
	// single bid1/ask1 level
	bid1Diff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	ask1Diff, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	bid1Vol, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	ask1Vol, ok := readVarintSafe(body, &off)
	if !ok {
		return Quote{}, off, false
	}
	q.Bids[0] = QuoteLevel{Price: quotePrice(closeRaw+bid1Diff, div), Volume: float64(bid1Vol)}
	q.Asks[0] = QuoteLevel{Price: quotePrice(closeRaw+ask1Diff, div), Volume: float64(ask1Vol)}
	// 56B fixed tail
	if off+56 > len(body) {
		return Quote{}, off, false
	}
	off += 56
	return q, off, true
}
