package go2tdx

import (
	"encoding/binary"
	"time"

	"github.com/chainball/go2tdx-lite/internal/gbk"
)

const (
	msgHeartbeat uint16 = 4
	msgHandshake uint16 = 13
)

type HandshakeInfo struct {
	ServerTime time.Time
	ServerName string
	ProductTag string
}

// Heartbeat keeps the connection alive and returns the server date (yyyymmdd).
func (a *SessionAPI) Heartbeat() (string, error) {
	body, err := a.c.pool.Exec(msgHeartbeat, nil)
	if err != nil {
		return "", err
	}
	return decodeHeartbeat(body)
}

// Handshake returns server time + name + product tag.
func (a *SessionAPI) Handshake() (HandshakeInfo, error) {
	body, err := a.c.pool.Exec(msgHandshake, []byte{0x01})
	if err != nil {
		return HandshakeInfo{}, err
	}
	return decodeHandshake(body)
}

// decodeHeartbeat reads the server date (yyyymmdd u32 LE) out of a heartbeat
// response body: 0..6 reserved | 6..10 server_date.
func decodeHeartbeat(body []byte) (string, error) {
	if len(body) < 10 {
		return "", errShortBody
	}
	date := binary.LittleEndian.Uint32(body[6:10])
	return padDate(date), nil
}

// decodeHandshake reads a handshake response body (>=189B):
// 0 unused u8 | 1 year u16 | 3 day u8 | 4 month u8 | 5 minute u8 | 6 hour u8 |
// 7 unused u8 | 8 second u8 | ... | 68..152 server_name GBK NUL-pad |
// 152..160 tail_control | 160..189 product_tag GBK NUL-pad.
func decodeHandshake(body []byte) (HandshakeInfo, error) {
	if len(body) < 189 {
		return HandshakeInfo{}, errShortBody
	}
	year := int(binary.LittleEndian.Uint16(body[1:3]))
	month := int(body[4])
	day := int(body[3])
	hour := int(body[6])
	minute := int(body[5])
	second := int(body[8])
	info := HandshakeInfo{
		ServerTime: time.Date(year, time.Month(month), day, hour, minute, second, 0, time.FixedZone("CST", 8*3600)),
	}
	if s, err := gbk.Decode(trimNUL(body[68:152])); err == nil {
		info.ServerName = s
	}
	if s, err := gbk.Decode(trimNUL(body[160:189])); err == nil {
		info.ProductTag = s
	}
	return info, nil
}
