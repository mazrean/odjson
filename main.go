// Command odjson generates reflection-free JSON encoders and decoders for Go
// struct types.
//
// The generated code implements encoding/json's Marshaler and Unmarshaler (and
// optionally encoding/json/v2's MarshalerTo and UnmarshalerFrom), so every
// major JSON library — encoding/json, encoding/json/v2, bytedance/sonic and
// goccy/go-json — picks it up automatically and gets faster without any change
// to the call sites.
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
	jsonV2          bool
	methods         bool
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
	fs.BoolVar(&cfg.caseInsensitive, "case-insensitive", true, "fall back to a case-insensitive field match, like encoding/json")
	fs.BoolVar(&cfg.jsonV2, "jsonv2", true, "also emit the encoding/json/v2 marshaler and unmarshaler methods")
	fs.BoolVar(&cfg.methods, "methods", false, "also emit MarshalJSON/UnmarshalJSON so existing call sites pick the generated codec up\n\twithout being changed. Off by default: the host library then re-validates the bytes the\n\tmethods return, which costs more than the generated codec saves for every library except\n\tencoding/json on small payloads. Prefer calling Marshal<T>/Unmarshal<T> directly")
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
			JSONV2:          cfg.jsonV2,
			Methods:         cfg.methods,
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
