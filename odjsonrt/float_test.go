package odjsonrt

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"testing"
)

// TestAppendFloatIntegralFastPath pins the integer shortcut against
// encoding/json for the values it can take and the boundaries around it.
func TestAppendFloatIntegralFastPath(t *testing.T) {
	values := []float64{
		0, math.Copysign(0, -1), 1, -1, 42, -42, 1e14, -1e14,
		999999999999999, -999999999999999, 1e15, -1e15, 1e15 + 1,
		1 << 53, -(1 << 53), 0.5, -0.5, 40.8, 1e-7, 1e21, 1e-300, 123456789.5,
	}
	for i := 0; i < 100000; i++ {
		values = append(values, float64(rand.Int64N(4e15)-2e15))
		values = append(values, rand.NormFloat64()*1e6)
	}
	for _, v := range values {
		got, err := AppendFloat(nil, v, 64)
		if err != nil {
			t.Fatalf("AppendFloat(%v): %v", v, err)
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal(%v): %v", v, err)
		}
		if string(got) != string(want) {
			t.Fatalf("AppendFloat(%v) = %q, want %q", v, got, want)
		}
	}
}
