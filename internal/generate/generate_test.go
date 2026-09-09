package generate_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mazrean/odjson/internal/generate"
)

// TestFixturesAreUpToDate regenerates every fixture package and fails when the
// committed output has drifted from what the current generator produces.
func TestFixturesAreUpToDate(t *testing.T) {
	cases := []struct {
		dir             string
		caseInsensitive bool
		command         string
	}{
		{dir: "../testfixture", caseInsensitive: true, command: "odjson -case-insensitive"},
		{dir: "../testfixture/embed", caseInsensitive: true, command: "odjson -case-insensitive"},
		{dir: "../testfixture/fallback", command: "odjson"},
		{dir: "../testfixture/crosspkg", command: "odjson"},
		{dir: "../testfixture/suite", command: "odjson"},
		{dir: "../testfixture/suitev2", command: "odjson"},
		{dir: "../testfixture/v2parity/gen", command: "odjson"},
		{dir: "../testfixture/withmethods", command: "odjson"},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			cfg := generate.Config{
				Output:          "odjson_gen.go",
				Recursive:       true,
				EscapeHTML:      true,
				CaseInsensitive: tc.caseInsensitive,
				Command:         tc.command,
			}
			got, err := generate.Source(tc.dir, cfg)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			path := filepath.Join(tc.dir, cfg.Output)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s is out of date; run go generate ./...", path)
			}
		})
	}
}

// TestGenerationIsIdempotent runs the generator twice over the same package and
// checks the second pass sees the first pass's methods and still produces the
// same file.
func TestGenerationIsIdempotent(t *testing.T) {
	cfg := generate.Config{
		Output:          "odjson_gen.go",
		Recursive:       true,
		EscapeHTML:      true,
		CaseInsensitive: true,
		Command:         "odjson",
	}
	first, err := generate.Source("../testfixture/withmethods", cfg)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := generate.Source("../testfixture/withmethods", cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("generation is not idempotent")
	}
}
