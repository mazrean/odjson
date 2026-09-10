// Command apicompat reports incompatible changes to odjson's public API.
//
// odjsonrt is what generated code imports, so a change to it breaks every
// tree that has already run the generator. This tool loads that package
// twice — once from a checkout of the base revision, once from the working
// tree — and prints the changes golang.org/x/exp/apidiff considers
// incompatible:
//
//	go tool apicompat -base /path/to/base-checkout github.com/mazrean/odjson/odjsonrt
//
// It exits non-zero when there is at least one incompatible change, so it can
// be used as a gate. Compatible changes (additions) are never reported: they
// are the normal case, and printing them makes the gate noisy.
//
// It lives in the github.com/mazrean/odjson/tools module rather than in the
// root one, so that golang.org/x/exp stays out of the graph that everyone
// importing odjsonrt downloads. The repository's go.work is what still lets
// the root module's `tool` shorthand reach it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/types"
	"io"
	"os"
	"strings"

	"golang.org/x/exp/apidiff"
	"golang.org/x/tools/go/packages"
)

func main() {
	base := flag.String("base", "", "directory holding a checkout of the revision to compare against (required)")
	dir := flag.String("dir", ".", "directory holding the revision under test")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), "usage: apicompat -base <dir> [-dir <dir>] <import-path>...\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *base == "" || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	incompatible, err := run(*base, *dir, flag.Args(), os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apicompat:", err)
		os.Exit(2)
	}
	if incompatible > 0 {
		os.Exit(1)
	}
}

// run compares each import path between the two directories and writes the
// incompatible changes to w, returning how many it found.
func run(baseDir, dir string, paths []string, w io.Writer) (int, error) {
	oldPkgs, err := load(baseDir, paths)
	if err != nil {
		return 0, fmt.Errorf("base revision: %w", err)
	}
	newPkgs, err := load(dir, paths)
	if err != nil {
		return 0, fmt.Errorf("revision under test: %w", err)
	}

	total := 0
	for _, path := range paths {
		oldPkg, newPkg := oldPkgs[path], newPkgs[path]
		switch {
		case oldPkg == nil:
			// A package that did not exist in the base revision is a pure
			// addition; there is nothing to break.
			continue
		case newPkg == nil:
			fmt.Fprintf(w, "%s: package removed\n", path)
			total++
			continue
		}

		var changes []string
		for _, c := range apidiff.Changes(oldPkg, newPkg).Changes {
			if !c.Compatible {
				changes = append(changes, strings.TrimSpace(c.Message))
			}
		}
		if len(changes) == 0 {
			continue
		}
		fmt.Fprintln(w, path)
		for _, m := range changes {
			fmt.Fprintf(w, "- %s\n", m)
		}
		total += len(changes)
	}
	return total, nil
}

// load type-checks the given import paths as they are in dir, keyed by import
// path. A path that does not resolve to a package there is left out rather
// than reported: only the caller knows whether that is a removal or an
// addition.
func load(dir string, paths []string) (map[string]*types.Package, error) {
	cfg := &packages.Config{
		Dir: dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedTypesSizes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, paths...)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", strings.Join(paths, " "), err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, errors.New("packages failed to load")
	}

	out := make(map[string]*types.Package, len(pkgs))
	for _, pkg := range pkgs {
		out[pkg.PkgPath] = pkg.Types
	}
	return out, nil
}
