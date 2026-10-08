package gbk

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	s := "平安银行"
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Fatalf("roundtrip = %q want %q", got, s)
	}
}

func TestDecodeKnownBytes(t *testing.T) {
	// fixture 里 security_list name 字段的 GBK 字节 = 平安银行
	b := []byte{0xC6, 0xBD, 0xB0, 0xB2, 0xD2, 0xF8, 0xD0, 0xD0}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != "平安银行" {
		t.Fatalf("Decode = %q want 平安银行", got)
	}
}

func TestEncodeASCIIPassthrough(t *testing.T) {
	// 6-byte security codes and exchange prefixes are ASCII; GBK leaves them
	// untouched.
	b, err := Encode("SZ000001")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, []byte("SZ000001")) {
		t.Fatalf("Encode = % x want % x", b, []byte("SZ000001"))
	}
}

func TestEncodeUnsupportedRune(t *testing.T) {
	// A rune outside the GBK repertoire must surface as an error rather than
	// silently sending a truncated name.
	if b, err := Encode("平😀"); err == nil {
		t.Fatalf("Encode = % x, want error", b)
	}
}

func TestDecodeInvalidBytesReplaced(t *testing.T) {
	got, err := Decode([]byte{0x41, 0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if got != "A�" {
		t.Fatalf("Decode = %q want %q", got, "A�")
	}
}
