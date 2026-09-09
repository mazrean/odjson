// Package generate drives odjson end to end: it analyses a package directory,
// renders the codec source and writes it back.
package generate

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/mazrean/odjson/internal/analyzer"
	"github.com/mazrean/odjson/internal/codegen"
)

// Config holds everything the generator needs for one package directory.
type Config struct {
	// Output is the file name written into the package directory.
	Output string
	// Types restricts generation to the named struct types.
	Types []string
	// Recursive also generates codecs for struct types reachable from the
	// selected ones.
	Recursive bool
	// EscapeHTML and CaseInsensitive are passed through to the code
	// generator.
	EscapeHTML      bool
	CaseInsensitive bool
	// Command is recorded in the generated file's header.
	Command string
}

// Source analyses dir and renders the generated file without writing it.
//
// A previously generated file is hidden from the type checker through a
// go/packages overlay rather than being moved aside: it declares the very
// methods the analyser uses to decide a type already has a hand written codec,
// so leaving it visible would make every regeneration a no-op. Using an
// overlay keeps the directory on disk untouched, which matters because other
// packages may be compiling against it at the same time.
func Source(dir string, cfg Config) ([]byte, error) {
	overlay, err := hide(filepath.Join(dir, cfg.Output))
	if err != nil {
		return nil, err
	}

	pkg, err := analyzer.Load(analyzer.Options{
		Dir:       dir,
		Pattern:   ".",
		Types:     cfg.Types,
		Recursive: cfg.Recursive,
		Overlay:   overlay,
	})
	if err != nil {
		return nil, err
	}
	return codegen.Generate(pkg, codegen.Options{
		EscapeHTML:      cfg.EscapeHTML,
		CaseInsensitive: cfg.CaseInsensitive,
		Command:         cfg.Command,
	})
}

// Run renders the generated file for dir and writes it into the package.
func Run(dir string, cfg Config) error {
	src, err := Source(dir, cfg)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, cfg.Output)
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// hide builds a go/packages overlay that replaces an existing generated file
// with an empty file in the same package.
func hide(path string) (map[string][]byte, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	name, err := packageClause(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{abs: []byte("package " + name + "\n")}, nil
}

// packageClause reads just the package name declared by a Go file.
func packageClause(path string) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("read the package clause of %s: %w", path, err)
	}
	return f.Name.Name, nil
}
