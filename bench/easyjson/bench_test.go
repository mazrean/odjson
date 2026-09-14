package easyjson

import (
	"fmt"
	"testing"

	ej "github.com/mailru/easyjson"

	"github.com/mazrean/odjson/bench/internal/harness"
)

// marshal and unmarshal reach the generated methods through easyjson's own
// entry points, which is the only way easyjson is used: with
// -no_std_marshalers the types carry no MarshalJSON / UnmarshalJSON.
func marshal(v any) ([]byte, error) {
	m, ok := v.(ej.Marshaler)
	if !ok {
		return nil, fmt.Errorf("easyjson: %T has no generated MarshalEasyJSON", v)
	}
	return ej.Marshal(m)
}

func unmarshal(data []byte, v any) error {
	u, ok := v.(ej.Unmarshaler)
	if !ok {
		return fmt.Errorf("easyjson: %T has no generated UnmarshalEasyJSON", v)
	}
	return ej.Unmarshal(data, u)
}

func payloads(tb testing.TB) []harness.Payload {
	tb.Helper()
	return harness.Load(tb, func() any { return new(TwitterStruct) }, func() any { return new(Book) })
}

// TestParity checks the generated code against encoding/json on every
// payload, so the rows below are measuring the same job.
func TestParity(t *testing.T) {
	harness.Parity(t, payloads(t), marshal, unmarshal)
}

func BenchmarkMarshal(b *testing.B) {
	harness.Marshal(b, "easyjson", payloads(b), marshal)
}

func BenchmarkUnmarshal(b *testing.B) {
	harness.Unmarshal(b, "easyjson", payloads(b), unmarshal)
}
