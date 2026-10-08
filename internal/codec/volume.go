package codec

import "math"

// DecodeVolume decodes a TDX get_volume u32 into a float (kline volume/amount).
func DecodeVolume(v uint32) float64 {
	s := int32(v)
	logpoint := s >> 24
	hleax := (s >> 16) & 0xFF
	lheax := (s >> 8) & 0xFF
	lleax := s & 0xFF
	base := math.Pow(2, float64(logpoint*2-0x7F))
	var high float64
	if hleax > 0x80 {
		high = base * float64(64+(hleax&0x7F)) / 64
	} else {
		high = base * float64(hleax) / 128
	}
	scale := 1.0
	if hleax&0x80 != 0 {
		scale = 2.0
	}
	middle := base * float64(lheax) / 32768 * scale
	low := base * float64(lleax) / 8388608 * scale
	return base + high + middle + low
}
