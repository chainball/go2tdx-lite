package go2tdx

import "time"

type Security struct {
	Code     string
	Name     string
	Multiple int
	Decimal  int
	PreClose float64
}

type Bar struct {
	Time                   time.Time
	Open, Close, High, Low float64
	Volume, Amount         float64
	UpCount, DownCount     int // 仅指数
}

type XdxrRecord struct {
	Market         uint16
	Code           string
	Date           string // yyyymmdd
	Category       int
	C1, C2, C3, C4 float64
}

type MinuteBar struct {
	Price, Avg, Volume float64
}

type AuxPoint struct {
	SeriesA, SeriesB int64
}

type Tick struct {
	Time       time.Time
	Price      float64
	Volume     int
	OrderCount int
	Status     int
}

type Auction struct {
	Time          time.Time
	Price         float64
	MatchedVolume uint32
	Unmatched     int32
}

type QuoteLevel struct {
	Price, Volume float64
}

type Quote struct {
	Market                                    uint16
	Code                                      string
	Active1                                   uint16
	Last, PreClose, Open, High, Low           float64
	TotalHand, CurrentHand                    int64
	Amount, InsideDish, OuterDisc, OpenAmount float64
	Bids, Asks                                [5]QuoteLevel
	Status                                    int64 // 交易状态(trading_status/status), 0=正常 非0=停牌等
}

type FinanceInfo struct {
	Market                                                              uint16
	Code                                                                string
	LiuTongGuBen                                                        float64
	Province, Industry                                                  uint16
	UpdatedDate, IPODate                                                string
	ZongGuBen, GuoJiaGu, FaQiRenFaRenGu, FaRenGu, BGu, HGu, EPS         float64
	ZongZiChan, LiuDongZiChan, GuDingZiChan, WuXingZiChan, GuDongRenShu float64
	LiuDongFuZhai, ChangQiFuZhai, ZiBenGongJiJin, JingZiChan            float64
	ZhuYingShouRu, ZhuYingLiRun, YingShouZhangKuan, YingYeLiRun         float64
	TouZiShouYu, JingYingXianJinLiu, ZongXianJinLiu, CunHuo             float64
	LiRunZongHe, ShuiHouLiRun, JingLiRun, WeiFenLiRun                   float64
	MeiGuJingZiChan, BaoLiu2                                            float64
}

