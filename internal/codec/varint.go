package codec

import "bytes"

// ReadVarint decodes one TDX signed varint. First byte: bits 0-5 magnitude,
// bit 6 sign, bit 7 continuation. Subsequent bytes: 7 bits each, shift 6,13,20...
// The sign bit applies to the whole magnitude, so negation happens after all
// continuation bytes are consumed.
// Returns the value and number of bytes consumed.
func ReadVarint(b []byte) (int64, int) {
	var v int64
	neg := false
	shift := uint(6)
	i := 0
	for {
		c := b[i]
		i++
		if i == 1 {
			v = int64(c & 0x3F)
			neg = c&0x40 != 0
			if c&0x80 == 0 {
				break
			}
		} else {
			v |= int64(c&0x7F) << shift
			shift += 7
			if c&0x80 == 0 {
				break
			}
		}
	}
	if neg {
		v = -v
	}
	return v, i
}

// WriteVarint writes v as a TDX signed varint.
func WriteVarint(buf *bytes.Buffer, v int64) {
	neg := v < 0
	m := v
	if neg {
		m = -m
	}
	first := byte(m & 0x3F)
	if neg {
		first |= 0x40
	}
	m >>= 6
	if m != 0 {
		first |= 0x80
	}
	buf.WriteByte(first)
	for m != 0 {
		b := byte(m & 0x7F)
		m >>= 7
		if m != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
	}
}

// ReadK reads the 5 price varints: current (absolute milli) followed by
// lastClose/open/high/low deltas, each relative to current.
func ReadK(b []byte) (cur, lastClose, open, high, low int64, n int) {
	cur, n = ReadVarint(b)
	var ds [4]int64
	for i := range ds {
		d, m := ReadVarint(b[n:])
		ds[i] = d
		n += m
	}
	return cur, cur + ds[0], cur + ds[1], cur + ds[2], cur + ds[3], n
}
