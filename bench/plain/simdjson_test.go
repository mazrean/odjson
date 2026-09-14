package plain

import (
	"testing"

	simdjson "github.com/minio/simdjson-go"

	"github.com/mazrean/odjson/bench/internal/harness"
)

// TestSimdjsonParity holds the hand-written tape walker in simdjson.go to
// encoding/json's reading of every payload, so the simdjson-go row is
// measuring the same job as the others.
func TestSimdjsonParity(t *testing.T) {
	if !simdjson.SupportedCPU() {
		t.Skip("simdjson-go needs AVX2 and CLMUL")
	}
	ps := harness.Load(t, func() any { return new(TwitterStruct) }, func() any { return new(Book) })
	harness.Parity(t, ps, nil, SimdjsonUnmarshal)
}
