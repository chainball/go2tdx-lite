package go2tdx

import "testing"

func TestDial(t *testing.T) {
	c, err := Dial(Server{Name: "fake1", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	if c == nil {
		t.Fatal("Dial returned nil client")
	}
	if c.pool == nil {
		t.Fatal("Dial did not initialize main pool")
	}
}

func TestCloseNilSafe(t *testing.T) {
	var c Client
	if err := c.Close(); err != nil {
		t.Fatalf("Close on zero-value Client returned error: %v", err)
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := []struct {
		in      string
		market  uint16
		code    string
		wantErr bool
	}{
		{"sz000001", MarketSZ, "000001", false},
		{"SH600000", MarketSH, "600000", false},
		{"000001", MarketSZ, "000001", false},
		{"600000", MarketSH, "600000", false},
		{"920001", MarketBJ, "920001", false},
		{"830001", MarketBJ, "830001", false},
		{"bj920001", MarketBJ, "920001", false},
		{" 000001 ", MarketSZ, "000001", false},
		{"bad", 0, "", true},
		{"12345", 0, "", true},
		{"", 0, "", true},
	}
	for _, tc := range cases {
		market, code, err := NormalizeCode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeCode(%q) expected error, got (%d, %q)", tc.in, market, code)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeCode(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if market != tc.market || code != tc.code {
			t.Errorf("NormalizeCode(%q) = (%d, %q), want (%d, %q)", tc.in, market, code, tc.market, tc.code)
		}
	}
}

func TestPriceDivisor(t *testing.T) {
	cases := []struct {
		code    string
		decimal int
		want    int
	}{
		{"000001", 2, 1},
		{"600000", 2, 1},
		{"159919", 2, 10}, // ETF prefix 15
		{"510300", 2, 10}, // ETF prefix 51
		{"000001", 3, 10},
		{"000001", 4, 100},
		{"000001", 5, 1000},
	}
	for _, tc := range cases {
		if got := PriceDivisor(tc.code, tc.decimal); got != tc.want {
			t.Errorf("PriceDivisor(%q, %d) = %d, want %d", tc.code, tc.decimal, got, tc.want)
		}
	}
}
