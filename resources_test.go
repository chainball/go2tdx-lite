package go2tdx

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"go2tdx/internal/frame"
)

// --- encode: file_content (0x06B9) ---

func TestEncodeFileContentRequest(t *testing.T) {
	meta := readFixtureMeta(t, "file_content", "normal")
	if meta.CommandCode != 1721 {
		t.Errorf("file_content/normal command_code = %d, want 1721", meta.CommandCode)
	}

	data, err := encodeFileContentRequest("zhb.zip", 10, 30000)
	if err != nil {
		t.Fatalf("encodeFileContentRequest: %v", err)
	}
	if len(data) != 308 {
		t.Fatalf("encodeFileContentRequest len = %d, want 308", len(data))
	}
	if got := binary.LittleEndian.Uint32(data[0:4]); got != 10 {
		t.Errorf("offset = %d, want 10", got)
	}
	if got := binary.LittleEndian.Uint32(data[4:8]); got != 30000 {
		t.Errorf("size = %d, want 30000", got)
	}
	// path is ASCII, NUL-padded to 300B.
	if got := data[8:308]; string(got[:7]) != "zhb.zip" || !isAllZero(got[7:]) {
		t.Errorf("path field = % x, want \"zhb.zip\" + 293 NULs", got[:16])
	}

	got := frame.EncodeRequest(meta.MessageID, msgFileContent, data)
	if want := sessionFixture(t, "file_content", "normal", "request.bin"); !bytes.Equal(got, want) {
		t.Fatalf("file_content/normal request = % x, want % x", got, want)
	}
}

func TestEncodeFileContentRequestNormalizesPath(t *testing.T) {
	data, err := encodeFileContentRequest(`vipdoc\sz\lday\zhb.zip`, 0, 60000)
	if err != nil {
		t.Fatalf("encodeFileContentRequest: %v", err)
	}
	p := string(trimNUL(data[8:]))
	if p != "vipdoc/sz/lday/zhb.zip" {
		t.Errorf("path = %q, want backslashes normalized to slashes", p)
	}
}

func TestEncodeFileContentRequestRejectsBadArgs(t *testing.T) {
	if _, err := encodeFileContentRequest("zhb.zip", 0, 0); err == nil {
		t.Error("size 0 error = nil, want range error")
	}
	if _, err := encodeFileContentRequest("zhb.zip", 0, 60001); err == nil {
		t.Error("size 60001 error = nil, want range error")
	}
	if _, err := encodeFileContentRequest(strings.Repeat("a", 301), 0, 30000); err == nil {
		t.Error("301-byte path error = nil, want length error")
	}
}

// --- decode: file_content ---

func TestDecodeFileContentFixture(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "file_content", "normal", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	if resp.MsgType != msgFileContent {
		t.Errorf("msg_type = %d, want %d", resp.MsgType, msgFileContent)
	}
	content, chunkLen, err := decodeFileContent(resp.Body)
	if err != nil {
		t.Fatalf("decodeFileContent: %v", err)
	}
	if chunkLen != 6 {
		t.Errorf("chunk_len = %d, want 6", chunkLen)
	}
	if len(content) != int(chunkLen) {
		t.Errorf("content len = %d, want %d", len(content), chunkLen)
	}

	// Golden: content hex 616263313233 ("abc123"), raw payload carries 2 trailing
	// bytes beyond chunk_len which must be ignored.
	g := goldenRoot(t, "file_content", "normal")
	rec := g.field(t, "chunk_len")
	if rec.Value != "6" {
		t.Errorf("golden chunk_len = %q, want \"6\"", rec.Value)
	}
	if want := g.field(t, "content").Hex; hex.EncodeToString(content) != want {
		t.Errorf("content = %s, want %s", hex.EncodeToString(content), want)
	}
}

func TestDecodeFileContentEmptyChunk(t *testing.T) {
	resp, err := frame.DecodeResponseBytes(sessionFixture(t, "file_content", "max_chunk_empty", "response.bin"))
	if err != nil {
		t.Fatalf("DecodeResponseBytes: %v", err)
	}
	content, chunkLen, err := decodeFileContent(resp.Body)
	if err != nil {
		t.Fatalf("decodeFileContent: %v", err)
	}
	if chunkLen != 0 || len(content) != 0 {
		t.Errorf("chunk_len/content = %d/%d, want 0/0", chunkLen, len(content))
	}
}

func TestDecodeFileContentShortBody(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3} {
		if _, _, err := decodeFileContent(make([]byte, n)); !errors.Is(err, errShortBody) {
			t.Errorf("decodeFileContent(%d bytes) error = %v, want errShortBody", n, err)
		}
	}
	// chunk_len claims more bytes than the payload holds.
	body := make([]byte, 4+3)
	binary.LittleEndian.PutUint32(body[0:4], 6)
	if _, _, err := decodeFileContent(body); !errors.Is(err, errShortBody) {
		t.Errorf("decodeFileContent(short content) error = %v, want errShortBody", err)
	}
}

// --- ReadFile chunk loop ---

func TestReadAllChunks(t *testing.T) {
	// Two full chunks then a short one.
	full := append(bytes.Repeat([]byte{'a'}, 30000), bytes.Repeat([]byte{'b'}, 30000)...)
	full = append(full, bytes.Repeat([]byte{'c'}, 120)...)

	var offsets []uint32
	fetch := func(offset, size uint32) ([]byte, uint32, error) {
		offsets = append(offsets, offset)
		if size != readFileChunkSize {
			t.Errorf("chunk size = %d, want %d", size, readFileChunkSize)
		}
		if int(offset) >= len(full) {
			return nil, 0, nil
		}
		end := min(int(offset+size), len(full))
		chunk := full[offset:end]
		return chunk, uint32(len(chunk)), nil
	}

	got, err := readAllChunks(readFileChunkSize, fetch)
	if err != nil {
		t.Fatalf("readAllChunks: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Errorf("readAllChunks len = %d, want %d", len(got), len(full))
	}
	want := []uint32{0, 30000, 60000}
	if len(offsets) != len(want) {
		t.Fatalf("fetch offsets = %v, want %v", offsets, want)
	}
	for i, o := range want {
		if offsets[i] != o {
			t.Fatalf("fetch offsets = %v, want %v", offsets, want)
		}
	}
}

func TestReadAllChunksExactMultiple(t *testing.T) {
	// Two exact chunks then an empty one: the loop must terminate on 0 < size.
	full := bytes.Repeat([]byte{'z'}, 60000)
	calls := 0
	fetch := func(offset, size uint32) ([]byte, uint32, error) {
		calls++
		if int(offset) >= len(full) {
			return nil, 0, nil
		}
		end := min(int(offset+size), len(full))
		return full[offset:end], uint32(end) - offset, nil
	}
	got, err := readAllChunks(readFileChunkSize, fetch)
	if err != nil {
		t.Fatalf("readAllChunks: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Errorf("readAllChunks len = %d, want %d", len(got), len(full))
	}
	if calls != 3 {
		t.Errorf("fetch calls = %d, want 3", calls)
	}
}

func TestReadAllChunksPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	fetch := func(offset, size uint32) ([]byte, uint32, error) {
		calls++
		if calls == 2 {
			return nil, 0, boom
		}
		return bytes.Repeat([]byte{'a'}, int(size)), size, nil
	}
	got, err := readAllChunks(readFileChunkSize, fetch)
	if !errors.Is(err, boom) {
		t.Fatalf("readAllChunks error = %v, want %v", err, boom)
	}
	if got != nil {
		t.Errorf("readAllChunks content = %d bytes, want nil on error", len(got))
	}
}

// --- public methods against a non-listening pool ---

func TestResourcesMethodsPropagateExecError(t *testing.T) {
	c, err := Dial(Server{Name: "dead", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Resources().Read("zhb.zip", 0, 30000); err == nil {
		t.Error("Read error = nil, want transport error")
	}
	if _, err := c.Resources().ReadFile("zhb.zip"); err == nil {
		t.Error("ReadFile error = nil, want transport error")
	}
}

func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
