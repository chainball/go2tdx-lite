package codec

import (
	"bytes"
	"math"
	"testing"
)

func TestReadVarint(t *testing.T) {
	cases := []struct {
		b    []byte
		want int64
		n    int
	}{
		{[]byte{0x01}, 1, 1},
		{[]byte{0x41}, -1, 1},
		{[]byte{0x81, 0x01}, 65, 2},
	}
	for _, c := range cases {
		got, n := ReadVarint(c.b)
		if got != c.want || n != c.n {
			t.Fatalf("ReadVarint(% x) = %d,%d want %d,%d", c.b, got, n, c.want, c.n)
		}
	}
}

func TestVarintRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 63, -63, 64, -64, 8191, 100000, -100000} {
		var buf bytes.Buffer
		WriteVarint(&buf, v)
		got, n := ReadVarint(buf.Bytes())
		if got != v || n != buf.Len() {
			t.Fatalf("roundtrip %d -> % x -> %d,%d", v, buf.Bytes(), got, n)
		}
	}
}

func TestReadK(t *testing.T) {
	// current=100, deltas +0,+10,-5,+3 => lastClose=100 open=110 high=95 low=103
	var buf bytes.Buffer
	WriteVarint(&buf, 100)
	WriteVarint(&buf, 0)
	WriteVarint(&buf, 10)
	WriteVarint(&buf, -5)
	WriteVarint(&buf, 3)
	cur, lc, o, h, lo, n := ReadK(buf.Bytes())
	if cur != 100 || lc != 100 || o != 110 || h != 95 || lo != 103 || n != buf.Len() {
		t.Fatalf("ReadK = %d,%d,%d,%d,%d,%d", cur, lc, o, h, lo, n)
	}
}

func TestDecodeVolume(t *testing.T) {
	// eltdx unit test: wire bytes 0x64 00 00 00 (u32 LE) -> 100, which the
	// documented get_volume formula decodes to 2^-127 * (1 + 100/8388608).
	if got, want := DecodeVolume(100), math.Pow(2, -127)*(1+100.0/8388608); math.Abs(got-want) > 1e-9 {
		t.Fatalf("DecodeVolume(100) = %v want %v", got, want)
	}
	// Fixture-verified (testdata/7709/snapshots amount_raw -> amount, and
	// testdata/7709/legacy_quotes amount_raw -> amount).
	if got := DecodeVolume(1320464192); got != 1515692032.0 {
		t.Fatalf("DecodeVolume(1320464192) = %v want 1515692032", got)
	}
	if got := DecodeVolume(12345678); got != 1.729997962244868e-38 {
		t.Fatalf("DecodeVolume(12345678) = %v want 1.729997962244868e-38", got)
	}
}

// TestVarintWireFixture pins the decoder against real eltdx wire bytes from
// testdata/7709/snapshots/normal/response.bin (record for sz000001):
//
//	time_raw            = 15329508 -> a4 a3 cf 0e   (multi-byte, shift 6/13/20)
//	unknown_after_time  =    -1093 -> c5 11         (multi-byte, negative)
//	total_hand          =  1399367 -> 87 e9 aa 01
func TestVarintWireFixture(t *testing.T) {
	cases := []struct {
		b    []byte
		want int64
	}{
		{[]byte{0xa4, 0xa3, 0xcf, 0x0e}, 15329508},
		{[]byte{0xc5, 0x11}, -1093},
		{[]byte{0x87, 0xe9, 0xaa, 0x01}, 1399367},
	}
	for _, c := range cases {
		got, n := ReadVarint(c.b)
		if got != c.want || n != len(c.b) {
			t.Fatalf("ReadVarint(% x) = %d,%d want %d,%d", c.b, got, n, c.want, len(c.b))
		}
		var buf bytes.Buffer
		WriteVarint(&buf, c.want)
		if !bytes.Equal(buf.Bytes(), c.b) {
			t.Fatalf("WriteVarint(%d) = % x want % x", c.want, buf.Bytes(), c.b)
		}
	}
}

// TestReadKWireFixture pins ReadK against the same snapshot record:
// current=1093 (10.93) with deltas -27,-28,0,-31 -> pre_close 1066, open 1065,
// high 1093, low 1062, consuming all 6 bytes.
func TestReadKWireFixture(t *testing.T) {
	b := []byte{0x85, 0x11, 0x5b, 0x5c, 0x00, 0x5f}
	cur, lc, o, h, lo, n := ReadK(b)
	if cur != 1093 || lc != 1066 || o != 1065 || h != 1093 || lo != 1062 || n != len(b) {
		t.Fatalf("ReadK(% x) = %d,%d,%d,%d,%d,%d", b, cur, lc, o, h, lo, n)
	}
}
