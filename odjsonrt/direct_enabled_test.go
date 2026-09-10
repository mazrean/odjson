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

// TestGoMinorVerified pins what the gate accepts, against a fixed list rather
// than against whatever verifiedGoMinors happens to hold: a release, a patch
// and a release candidate of a listed minor, and nothing else. The last two
// rows are the reason this is not strings.HasPrefix.
func TestGoMinorVerified(t *testing.T) {
	saved := verifiedGoMinors
	verifiedGoMinors = []string{"go1.27", "go1.29"}
	t.Cleanup(func() { verifiedGoMinors = saved })

	for _, tt := range []struct {
		version string
		want    bool
	}{
		{"go1.27", true},
		{"go1.27.1", true},
		{"go1.27rc1", true},
		{"go1.29beta2", true},
		{"go1.28", false},
		{"go1.28.3", false},
		{"go1.270", false},             // a prefix test would take this
		{"devel go1.27-abcdef", false}, // and this is not a release at all
		{"", false},
	} {
		t.Run(tt.version, func(t *testing.T) {
			if got := goMinorVerified(tt.version); got != tt.want {
				t.Errorf("goMinorVerified(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
