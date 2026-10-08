package go2tdx

// Period identifies a kline period (raw matches eltdx/pytdx KLINE_TYPE).
type Period struct{ Raw, Param uint16 }

var (
	Min1    = Period{7, 1}
	Min5    = Period{0, 1}
	Min15   = Period{1, 1}
	Min30   = Period{2, 1}
	Min60   = Period{3, 1}
	Day     = Period{4, 1}
	Week    = Period{5, 1}
	Month   = Period{6, 1}
	Quarter = Period{10, 1}
	Year    = Period{11, 1}
)

const (
	AdjustNone uint16 = 0
	AdjustQFQ  uint16 = 1
	AdjustHFQ  uint16 = 2
)
