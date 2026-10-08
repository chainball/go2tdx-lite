// Package gbk converts between GBK and UTF-8. Security names and server names
// on the 7709 wire are GBK-encoded, so every string field crosses this package.
package gbk

import "golang.org/x/text/encoding/simplifiedchinese"

// Encode converts the UTF-8 string s to GBK bytes. It reports an error if s
// contains a rune that GBK cannot represent.
func Encode(s string) ([]byte, error) {
	return simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
}

// Decode converts GBK bytes b to a UTF-8 string. Bytes that are not valid GBK
// are replaced with U+FFFD.
func Decode(b []byte) (string, error) {
	p, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
	if err != nil {
		return "", err
	}
	return string(p), nil
}
