package go2tdx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

const msgFileContent uint16 = 1721 // 0x06B9

const (
	// fileContentPathLen is the fixed path field width of the request.
	fileContentPathLen = 300
	// fileContentRequestLen is the fixed 308B file_content request:
	// offset u32 | size u32 | path ASCII NUL-padded to 300B.
	fileContentRequestLen = 8 + fileContentPathLen
	// minFileChunkSize/maxFileChunkSize bound one read's size field.
	minFileChunkSize = 1
	maxFileChunkSize = 60000
	// readFileChunkSize is the size ReadFile requests per iteration.
	readFileChunkSize = 30000
)

// Read returns up to size bytes of the server-side file at path, starting at
// offset. A returned slice shorter than size means the end of the file.
func (a *ResourcesAPI) Read(path string, offset, size uint32) ([]byte, error) {
	content, _, err := a.readChunk(path, offset, size)
	return content, err
}

// ReadFile downloads path in full, looping in readFileChunkSize chunks until a
// short chunk (chunk_len < size) marks the end of the file.
func (a *ResourcesAPI) ReadFile(path string) ([]byte, error) {
	return readAllChunks(readFileChunkSize, func(offset, size uint32) ([]byte, uint32, error) {
		return a.readChunk(path, offset, size)
	})
}

// readChunk performs one file_content round trip and reports both the content
// and the wire chunk_len (which may be shorter than size on the last chunk).
func (a *ResourcesAPI) readChunk(path string, offset, size uint32) ([]byte, uint32, error) {
	data, err := encodeFileContentRequest(path, offset, size)
	if err != nil {
		return nil, 0, err
	}
	body, err := a.c.pool.Exec(msgFileContent, data)
	if err != nil {
		return nil, 0, err
	}
	return decodeFileContent(body)
}

// chunkFetcher reads one chunk at offset with the given size, returning the
// content and the wire chunk_len.
type chunkFetcher func(offset, size uint32) ([]byte, uint32, error)

// readAllChunks drives the ReadFile loop: start at offset 0 and advance by
// chunk_len until a chunk shorter than the requested size ends the file. A
// chunk_len of 0 always terminates, so an empty file cannot loop forever.
func readAllChunks(chunkSize uint32, fetch chunkFetcher) ([]byte, error) {
	var out []byte
	offset := uint32(0)
	for {
		content, chunkLen, err := fetch(offset, chunkSize)
		if err != nil {
			return nil, err
		}
		out = append(out, content...)
		if chunkLen < chunkSize {
			return out, nil
		}
		offset += chunkLen
	}
}

// encodeFileContentRequest builds the 308B file_content request: offset u32 |
// size u32 (1..60000) | path ASCII NUL-padded to 300B. Backslashes in path are
// normalized to forward slashes.
func encodeFileContentRequest(path string, offset, size uint32) ([]byte, error) {
	if size < minFileChunkSize || size > maxFileChunkSize {
		return nil, fmt.Errorf("go2tdx: file_content size %d out of range %d..%d", size, minFileChunkSize, maxFileChunkSize)
	}
	p := strings.ReplaceAll(path, `\`, "/")
	if len(p) > fileContentPathLen {
		return nil, fmt.Errorf("go2tdx: file_content path too long (%d > %d)", len(p), fileContentPathLen)
	}
	data := make([]byte, fileContentRequestLen)
	binary.LittleEndian.PutUint32(data[0:4], offset)
	binary.LittleEndian.PutUint32(data[4:8], size)
	copy(data[8:], p)
	return data, nil
}

// decodeFileContent reads a file_content response body: 4B chunk_len u32 +
// content bytes. Bytes beyond chunk_len are ignored.
func decodeFileContent(body []byte) ([]byte, uint32, error) {
	if len(body) < 4 {
		return nil, 0, errShortBody
	}
	chunkLen := binary.LittleEndian.Uint32(body[0:4])
	if uint64(len(body)-4) < uint64(chunkLen) {
		return nil, 0, errShortBody
	}
	return bytes.Clone(body[4 : 4+chunkLen]), chunkLen, nil
}
