package gojay

import (
	"fmt"
	"testing"

	"github.com/francoispqt/gojay"

	"github.com/mazrean/odjson/bench/internal/harness"
)

// marshal and unmarshal go through gojay's own entry points, which dispatch on
// the MarshalerJSONObject / UnmarshalerJSONObject interfaces codec.go
// implements.
func marshal(v any) ([]byte, error) {
	m, ok := v.(gojay.MarshalerJSONObject)
	if !ok {
		return nil, fmt.Errorf("gojay: %T does not implement MarshalerJSONObject", v)
	}
	return gojay.MarshalJSONObject(m)
}

func unmarshal(data []byte, v any) error {
	u, ok := v.(gojay.UnmarshalerJSONObject)
	if !ok {
		return fmt.Errorf("gojay: %T does not implement UnmarshalerJSONObject", v)
	}
	return gojay.UnmarshalJSONObject(data, u)
}

func payloads(tb testing.TB) []harness.Payload {
	tb.Helper()
	return harness.Load(tb, func() any { return new(TwitterStruct) }, func() any { return new(Book) })
}

// TestParity holds the hand-written code in codec.go to encoding/json's
// reading of every payload: what it decodes must be the same value, and what
// it encodes must read back as the same value.
func TestParity(t *testing.T) {
	harness.Parity(t, payloads(t), marshal, unmarshal)
}

func BenchmarkMarshal(b *testing.B) {
	harness.Marshal(b, "gojay", payloads(b), marshal)
}

func BenchmarkUnmarshal(b *testing.B) {
	harness.Unmarshal(b, "gojay", payloads(b), unmarshal)
}
