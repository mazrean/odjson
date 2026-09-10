// Command lint is the canonical linter for the odjson repository.
//
// It bundles, into a single binary:
//
//   - the analyzer suite that "go vet" runs
//     (golang.org/x/tools/go/analysis/suite/vet),
//   - staticcheck's SA* checks (honnef.co/go/tools/staticcheck),
//   - stylecheck's ST* checks (honnef.co/go/tools/stylecheck).
//
// Analyzers that staticcheck marks as non-default (opt-in, e.g. ST1000
// "at least one file in a package should have a package comment") are
// skipped so that the default behaviour matches upstream staticcheck.
//
// It lives in the github.com/mazrean/odjson/tools module rather than in the
// root one, so that staticcheck and its dependencies stay out of the graph
// that everyone importing odjsonrt downloads. The repository's go.work is
// what still lets the root module's `tool` shorthand reach it:
//
//	go tool lint ./... ./tools/...
package main

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"
	"golang.org/x/tools/go/analysis/suite/vet"

	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
)

func main() {
	analyzers := make([]*analysis.Analyzer, 0, len(vet.Suite)+len(staticcheck.Analyzers)+len(stylecheck.Analyzers))

	// go vet's analyzers, kept in sync with the toolchain by x/tools.
	analyzers = append(analyzers, vet.Suite...)

	// staticcheck + stylecheck, minus the checks upstream disables by default.
	analyzers = append(analyzers, defaultAnalyzers(staticcheck.Analyzers)...)
	analyzers = append(analyzers, defaultAnalyzers(stylecheck.Analyzers)...)

	multichecker.Main(analyzers...)
}

// defaultAnalyzers unwraps honnef.co analyzers, dropping the ones that are
// not enabled in staticcheck's default configuration.
func defaultAnalyzers(as []*lint.Analyzer) []*analysis.Analyzer {
	out := make([]*analysis.Analyzer, 0, len(as))
	for _, a := range as {
		if a.Doc != nil && a.Doc.NonDefault {
			continue
		}
		out = append(out, a.Analyzer)
	}
	return out
}
