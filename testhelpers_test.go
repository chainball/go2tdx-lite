package go2tdx

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fixtureMeta is the top-level subset of a fixture metadata.json.
type fixtureMeta struct {
	CommandCode uint16 `json:"command_code"`
	MessageID   uint32 `json:"message_id"`
}

// readFixtureMeta reads testdata/7709/<cmd>/<sub>/metadata.json.
func readFixtureMeta(t *testing.T, cmd, sub string) fixtureMeta {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, sub, "metadata.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read metadata %s: %v", p, err)
	}
	var m fixtureMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse metadata %s: %v", p, err)
	}
	return m
}

// goldenValue is one node of the eltdx golden schema: objects carry a "$type",
// scalars carry "value", bytes carry "hex", f32 scalars carry "wire_f32_bits",
// dataclasses carry "fields" as [name, value] pairs, containers carry "items".
type goldenValue struct {
	Type    string               `json:"$type"`
	Value   string               `json:"value"`
	Hex     string               `json:"hex"`
	WireF32 string               `json:"wire_f32_bits"`
	Fields  [][2]json.RawMessage `json:"fields"`
	Items   []json.RawMessage    `json:"items"`
}

// field returns the named member of a dataclass node.
func (v *goldenValue) field(t *testing.T, name string) *goldenValue {
	t.Helper()
	for _, p := range v.Fields {
		var n string
		if err := json.Unmarshal(p[0], &n); err != nil {
			t.Fatalf("golden field name: %v", err)
		}
		if n != name {
			continue
		}
		var out goldenValue
		if err := json.Unmarshal(p[1], &out); err != nil {
			t.Fatalf("golden field %q: %v", name, err)
		}
		return &out
	}
	t.Fatalf("golden field %q not found", name)
	return nil
}

// item returns the i-th element of a container node.
func (v *goldenValue) item(t *testing.T, i int) *goldenValue {
	t.Helper()
	if i < 0 || i >= len(v.Items) {
		t.Fatalf("golden item %d out of range (len %d)", i, len(v.Items))
	}
	var out goldenValue
	if err := json.Unmarshal(v.Items[i], &out); err != nil {
		t.Fatalf("golden item %d: %v", i, err)
	}
	return &out
}

// goldenRoot reads testdata/7709/<cmd>/<sub>/expected.json.
func goldenRoot(t *testing.T, cmd, sub string) *goldenValue {
	t.Helper()
	p := filepath.Join("testdata", "7709", cmd, sub, "expected.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read expected %s: %v", p, err)
	}
	var v goldenValue
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("parse expected %s: %v", p, err)
	}
	return &v
}

// wireF32 converts a golden "wire_f32_bits" hex string to the decoded float64.
func wireF32(t *testing.T, bits string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(bits, 16, 32)
	if err != nil {
		t.Fatalf("wire_f32_bits %q: %v", bits, err)
	}
	return float64(math.Float32frombits(uint32(v)))
}
