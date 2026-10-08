package go2tdx

import (
	"errors"
	"strings"
)

const (
	MarketSZ uint16 = 0
	MarketSH uint16 = 1
	MarketBJ uint16 = 2
)

// NormalizeCode maps "sz000001"/"sh600000"/"bj920001"/"000001" to (market, 6-digit code).
func NormalizeCode(code string) (uint16, string, error) {
	code = strings.TrimSpace(code)
	if len(code) == 8 {
		prefix := strings.ToLower(code[:2])
		digits := code[2:]
		if isDigits(digits) {
			switch prefix {
			case "sz":
				return MarketSZ, digits, nil
			case "sh":
				return MarketSH, digits, nil
			case "bj":
				return MarketBJ, digits, nil
			}
		}
	}
	if len(code) == 6 && isDigits(code) {
		switch code[0] {
		case '9':
			if code[1] == '2' {
				return MarketBJ, code, nil
			}
			return MarketSH, code, nil
		case '6':
			return MarketSH, code, nil
		case '8':
			return MarketBJ, code, nil
		default:
			return MarketSZ, code, nil
		}
	}
	return 0, "", errors.New("go2tdx: invalid code " + code)
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// PriceDivisor converts wire milli-prices to real prices for a code.
func PriceDivisor(code6 string, decimal int) int {
	switch code6[:2] {
	case "15", "16", "50", "51", "52", "53", "56", "58":
		return 10
	}
	switch {
	case decimal <= 2:
		return 1
	case decimal == 3:
		return 10
	case decimal == 4:
		return 100
	default:
		d := 1
		for i := 0; i < decimal-2; i++ {
			d *= 10
		}
		return d
	}
}
