//go:build !odjson_safe

package odjsonrt

import (
	"runtime"
	"testing"
)

// TestDirectEnabled pins the direct path to the Go versions it was verified
// against: on one of those it must come up, so that a silent fallback to the
// public API path cannot hide behind passing tests. It reads the same
// verifiedGoMinors the gate does, so widening the gate is what makes this
// assertion bite on a new release instead of skipping past it.
func TestDirectEnabled(t *testing.T) {
	if !goMinorVerified(runtime.Version()) {
		t.Skipf("direct path is gated to %v, running %s", verifiedGoMinors, runtime.Version())
	}
	if !DirectEnabled() {
		t.Fatal("the direct path is disabled on the Go version it was verified against")
	}
}
