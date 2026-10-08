package frame

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
)

const RequestHeaderSize = 12
const ResponseHeaderSize = 16

var respPrefix = []byte{0xB1, 0xCB, 0x74, 0x00}

type Response struct {
	Control byte
	MsgID   uint32
	MsgType uint16
	Body    []byte
}

// EncodeRequest builds a 12-byte-header request frame.
func EncodeRequest(msgID uint32, msgType uint16, data []byte) []byte {
	n := len(data) + 2
	buf := make([]byte, RequestHeaderSize+len(data))
	buf[0] = 0x0C
	binary.LittleEndian.PutUint32(buf[1:5], msgID)
	buf[5] = 0x01
	binary.LittleEndian.PutUint16(buf[6:8], uint16(n))
	binary.LittleEndian.PutUint16(buf[8:10], uint16(n))
	binary.LittleEndian.PutUint16(buf[10:12], msgType)
	copy(buf[12:], data)
	return buf
}

// DecodeResponseBytes parses one response frame from a byte slice, resynchronizing
// on the 4-byte prefix. Returns error if no prefix or bad compression.
func DecodeResponseBytes(b []byte) (*Response, error) {
	i := bytes.Index(b, respPrefix)
	if i < 0 {
		return nil, errors.New("frame: response prefix not found")
	}
	b = b[i:]
	if len(b) < ResponseHeaderSize {
		return nil, errors.New("frame: short header")
	}
	r := &Response{
		Control: b[4],
		MsgID:   binary.LittleEndian.Uint32(b[5:9]),
		MsgType: binary.LittleEndian.Uint16(b[10:12]),
	}
	zipLen := int(binary.LittleEndian.Uint16(b[12:14]))
	rawLen := int(binary.LittleEndian.Uint16(b[14:16]))
	if len(b) < ResponseHeaderSize+zipLen {
		return nil, errors.New("frame: short payload")
	}
	payload := b[ResponseHeaderSize : ResponseHeaderSize+zipLen]
	if zipLen == rawLen {
		r.Body = payload
		return r, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	if len(body) != rawLen {
		return nil, errors.New("frame: decompressed size mismatch")
	}
	r.Body = body
	return r, nil
}
