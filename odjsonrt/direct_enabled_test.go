//go:build !odjson_safe

package odjsonrt

import (
	"runtime"
	"strings"
	"testing"
)

// TestDirectEnabled pins the direct path to the Go version it was verified
// against: on that version it must come up, so that a silent fallback to the
// public API path cannot hide behind passing tests.
func TestDirectEnabled(t *testing.T) {
	if !strings.HasPrefix(runtime.Version(), "go1.27") {
		t.Skipf("direct path is gated to Go 1.27, running %s", runtime.Version())
	}
	if !DirectEnabled() {
		t.Fatal("the direct path is disabled on the Go version it was verified against")
	}
}
