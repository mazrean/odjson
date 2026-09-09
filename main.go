// Command odjson generates reflection-free JSON encoders and decoders for Go
// struct types.
//
// The generated code implements encoding/json's Marshaler and Unmarshaler and
// encoding/json/v2's MarshalerTo and UnmarshalerFrom, so an unchanged
// json.Marshal or json.Unmarshal routes through it. It is bolted onto the
// standard libraries rather than replacing them: delete the generated file and
// every call site keeps working, at the library's own speed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/mazrean/odjson/internal/analyzer"
	"github.com/mazrean/odjson/internal/generate"
)

const usage = `odjson generates reflection-free JSON codecs for Go structs.

usage: odjson [flags] [packages]

With no package argument odjson generates code for the package in the current
directory, which is what //go:generate needs.

flags:
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "odjson: %v\n", err)
		os.Exit(1)
	}
}

type config struct {
	types           string
	output          string
	escapeHTML      bool
	caseInsensitive bool
	recursive       bool
	showVersion     bool
}

func run(args []string) error {
	var cfg config
	fs := flag.NewFlagSet("odjson", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	fs.StringVar(&cfg.types, "type", "", "comma separated list of struct types to generate for (default: every exported struct in the package)")
	fs.StringVar(&cfg.output, "output", "odjson_gen.go", "name of the generated file, written into each matched package directory")
	fs.BoolVar(&cfg.escapeHTML, "escape-html", true, "escape <, > and & like encoding/json does by default")
	fs.BoolVar(&cfg.caseInsensitive, "case-insensitive", false, "in UnmarshalJSON, fall back to a case-insensitive field match the way encoding/json v1\n\tdoes. Off by default, matching encoding/json/v2")
	fs.BoolVar(&cfg.recursive, "recursive", true, "also generate codecs for struct types reachable from the selected types")
	fs.BoolVar(&cfg.showVersion, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if cfg.showVersion {
		fmt.Println(version())
		return nil
	}
	if filepath.Base(cfg.output) != cfg.output {
		return fmt.Errorf("-output must be a file name, not a path: %s", cfg.output)
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	var types []string
	if cfg.types != "" {
		types = strings.Split(cfg.types, ",")
		for i := range types {
			types[i] = strings.TrimSpace(types[i])
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	var dirs []string
	for _, pattern := range patterns {
		found, err := analyzer.ListDirs(wd, pattern)
		if err != nil {
			return err
		}
		dirs = append(dirs, found...)
	}
	if len(types) > 0 && len(dirs) > 1 {
		return errors.New("-type may only be used with a single package")
	}

	for _, dir := range dirs {
		gcfg := generate.Config{
			Output:          cfg.output,
			Types:           types,
			Recursive:       cfg.recursive,
			EscapeHTML:      cfg.escapeHTML,
			CaseInsensitive: cfg.caseInsensitive,
			Command:         command(),
		}
		if err := generate.Run(dir, gcfg); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
	}
	return nil
}

func command() string {
	return "odjson " + strings.Join(os.Args[1:], " ")
}

// version reports the module version recorded in the binary, which goreleaser
// stamps from the git tag at build time.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		var rev, dirty string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return "devel-" + rev + dirty
		}
		return "(devel)"
	}
	return v
}
