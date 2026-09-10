// Command chart renders the benchmark tables in the root README as a pair of
// SVG small-multiple bar charts, one for each colour scheme.
//
// The numbers below are the same ones the README's tables carry. They are
// literals on purpose: the tables are the source of truth, and re-measuring
// means editing both. Regenerate with:
//
//	go run ./chart
//
// from the bench module root.
//
// The same renderer also draws a chart for numbers measured elsewhere — CI
// runs it on what a GitHub-hosted runner just produced:
//
//	go run ./chart -input bench.txt -out out -footer '...'
//
// -input reads `go test -bench` output, takes the median per benchmark name
// and overwrites the literals below. Those numbers are not the README's:
// say where they came from in -footer, because the caption is the only place
// the chart admits which machine it is describing.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// A row is one library's measurement within a panel, in the panel's unit.
type row struct {
	label    string
	value    float64
	indent   bool // drawn as a variant of the row above it
	odjson   bool // the highlighted bar
	emphasis bool // label in primary ink rather than secondary

	// Where -input takes this row's number from: the bench sub-module
	// (`plain` measures the libraries as they ship, `gen` measures them on
	// odjson-generated types) and the codec label the benchmark uses.
	module, codec string
}

// A panel is one benchmark: four libraries on a scale of their own.
type panel struct {
	title string
	sub   string
	unit  string
	ratio string // odjson against encoding/json/v2, as the README's tables state it
	rows  []row

	// Which benchmark -input reads for this panel, matching the names in
	// `go test -bench` output: BenchmarkMarshal/<codec>/<payload>.
	payload string
}

var panels = []panel{
	{
		title: "Marshal", sub: "large · 616 KiB", unit: "µs", ratio: "3.56", payload: "twitter",
		rows: []row{
			{label: "encoding/json/v2", value: 399, module: "plain", codec: "json-v2"},
			{label: "+ odjson", value: 112, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
			{label: "sonic", value: 114, module: "plain", codec: "sonic"},
			{label: "go-json", value: 243, module: "plain", codec: "go-json"},
		},
	},
	{
		title: "Marshal", sub: "small · 340 B", unit: "ns", ratio: "3.32", payload: "small",
		rows: []row{
			{label: "encoding/json/v2", value: 1060, module: "plain", codec: "json-v2"},
			{label: "+ odjson", value: 319, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
			{label: "sonic", value: 312, module: "plain", codec: "sonic"},
			{label: "go-json", value: 421, module: "plain", codec: "go-json"},
		},
	},
	{
		title: "Unmarshal", sub: "large · 616 KiB", unit: "µs", ratio: "2.12", payload: "twitter",
		rows: []row{
			{label: "encoding/json/v2", value: 1080, module: "plain", codec: "json-v2"},
			{label: "+ odjson", value: 508, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
			{label: "sonic", value: 513, module: "plain", codec: "sonic"},
			{label: "go-json", value: 672, module: "plain", codec: "go-json"},
		},
	},
	{
		title: "Unmarshal", sub: "small · 340 B", unit: "ns", ratio: "3.32", payload: "small",
		rows: []row{
			{label: "encoding/json/v2", value: 1864, module: "plain", codec: "json-v2"},
			{label: "+ odjson", value: 562, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
			{label: "sonic", value: 951, module: "plain", codec: "sonic"},
			{label: "go-json", value: 794, module: "plain", codec: "go-json"},
		},
	},
}

// A theme is the set of colour roles the chart is written against. Both are
// selected for their own surface rather than one being a flip of the other;
// the accent and the neutral clear CVD ΔE 16 and 3:1 contrast in both.
type theme struct {
	name      string
	surface   string
	primary   string // text
	secondary string // text
	accent    string // the odjson bar
	neutral   string // every other bar
	rule      string
}

var themes = []theme{
	{
		name:    "light",
		surface: "#fcfcfb", primary: "#0b0b0b", secondary: "#52514e",
		accent: "#2a78d6", neutral: "#82827a", rule: "#e4e3df",
	},
	{
		name:    "dark",
		surface: "#1a1a19", primary: "#ffffff", secondary: "#c3c2b7",
		accent: "#3987e5", neutral: "#8c8c84", rule: "#33332f",
	},
}

// Layout, in user units.
const (
	svgW = 912

	padX      = 20
	panelW    = 424
	panelGapX = 24
	panelGapY = 26

	labelW = 132 // library names
	valueW = 62  // the direct label after each bar
	gutter = 10
	barsW  = panelW - labelW - valueW - 2*gutter

	barH   = 18
	rowH   = 28
	radius = 4

	headerH = 62
	titleH  = 34 // panel title + subtitle
	footerH = 30
)

// readmeFooter describes the machine the literals above were measured on. Any
// other set of numbers needs its own caption, via -footer.
const readmeFooter = "Medians of 10 runs · AMD Ryzen 9 7950X · Linux · Go 1.27.1 · bench/plain and bench/gen"

func main() {
	input := flag.String("input", "", "`go test -bench` output to take the numbers from; the README's literals are used when empty")
	out := flag.String("out", "", "directory to write bench-light.svg and bench-dark.svg into (default docs/assets)")
	footer := flag.String("footer", readmeFooter, "the caption under the chart, naming where the numbers come from")
	summary := flag.String("summary", "", "also write the same numbers to this file as a Markdown table")
	flag.Parse()

	if *input != "" {
		f, err := os.Open(*input)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		measured, err := parse(f)
		if err != nil {
			fail(err)
		}
		if err := apply(measured); err != nil {
			fail(err)
		}
	}

	dir := *out
	if dir == "" {
		// Anchored on this file rather than the working directory, so running
		// it from bench/chart writes the same place as running it from bench/.
		self, ok := sourceDir()
		if !ok {
			fail(errors.New("cannot locate the source directory"))
		}
		dir = filepath.Join(self, "..", "..", "docs", "assets")
	}
	if _, err := os.Stat(dir); err != nil {
		fail(err)
	}
	for _, t := range themes {
		name := filepath.Join(dir, "bench-"+t.name+".svg")
		if err := os.WriteFile(name, render(t, *footer), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", name)
	}

	if *summary != "" {
		if err := os.WriteFile(*summary, table(*footer), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", *summary)
	}
}

// table restates the chart as Markdown. A PNG of the chart carries no alt
// text, so wherever the image goes this goes with it.
func table(footer string) []byte {
	var b bytes.Buffer
	b.WriteString("| Benchmark |")
	for _, r := range panels[0].rows {
		fmt.Fprintf(&b, " %s |", r.label)
	}
	b.WriteString("\n| --- |")
	for range panels[0].rows {
		b.WriteString(" ---: |")
	}
	b.WriteByte('\n')

	for _, p := range panels {
		fmt.Fprintf(&b, "| %s · %s |", p.title, p.sub)
		for _, r := range p.rows {
			if r.odjson {
				fmt.Fprintf(&b, " **%s %s** (%s×) |", fmtVal(r.value), p.unit, p.ratio)
				continue
			}
			fmt.Fprintf(&b, " %s %s |", fmtVal(r.value), p.unit)
		}
		b.WriteByte('\n')
	}

	// The same sentence the panels carry, for the same reason: a bare ratio
	// does not say what it is against.
	if i := slices.IndexFunc(panels[0].rows, func(r row) bool { return r.odjson }); i > 0 {
		fmt.Fprintf(&b, "\n× is odjson vs %s.\n", panels[0].rows[i-1].label)
	}
	fmt.Fprintf(&b, "\n%s\n", footer)
	return b.Bytes()
}

func sourceDir() (string, bool) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Dir(self), true
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "chart:", err)
	os.Exit(1)
}

func render(t theme, footer string) []byte {
	panelH := titleH + rowH*len(panels[0].rows)
	svgH := headerH + 2*panelH + panelGapY + footerH

	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s">`,
		svgW, svgH, svgW, svgH, esc(altText()))
	fmt.Fprintf(&b, `<style>
text{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace}
.t1{fill:%s}.t2{fill:%s}
</style>`, t.primary, t.secondary)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`, svgW, svgH, t.surface)

	fmt.Fprintf(&b, `<text class="t1" x="%d" y="26" font-size="16" font-weight="600">encoding/json/v2, with and without odjson</text>`, padX)
	fmt.Fprintf(&b, `<text class="t2" x="%d" y="46" font-size="12">Time per operation — lower is better. Each panel has its own scale.</text>`, padX)

	for i, p := range panels {
		x := padX + (i%2)*(panelW+panelGapX)
		y := headerH + (i/2)*(panelH+panelGapY)
		drawPanel(&b, t, p, x, y)
	}

	fmt.Fprintf(&b, `<text class="t2" x="%d" y="%d" font-size="11">%s</text>`,
		padX, svgH-11, esc(footer))
	b.WriteString(`</svg>`)
	b.WriteByte('\n')
	return b.Bytes()
}

func drawPanel(b *bytes.Buffer, t theme, p panel, x, y int) {
	max := 0.0
	for _, r := range p.rows {
		if r.value > max {
			max = r.value
		}
	}
	barX := x + labelW + gutter

	// The point of the whole chart, so it is the largest thing in the panel,
	// drawn in the accent — the colour of the bar it is about. It belongs
	// beside odjson's own time, which needs the space after that time to be
	// free; every measured ratio leaves it so, since a bar 2x shorter than
	// the longest ends before the panel's midpoint. A ratio near 1 would not,
	// and then the panel header takes it instead of overprinting the time.
	ratio := p.ratio + "× faster"
	ratioX, ratioInHeader := 0, true
	// The row the ratio is against: the one odjson is drawn indented under.
	baseline, baseW := "", 0
	if i := slices.IndexFunc(p.rows, func(r row) bool { return r.odjson }); i >= 0 {
		w := barW(p.rows[i].value, max)
		ratioX = barX + w + gutter + len(fmtVal(p.rows[i].value))*7 + 10
		// The estimate only decides which of the two places the text takes;
		// nothing is drawn to its width, so a few pixels out costs nothing.
		ratioInHeader = ratioX+textW(ratio, 16) > x+panelW
		if i > 0 {
			baseline = p.rows[i-1].label
			baseW = barW(p.rows[i-1].value, max)
		}
	}

	fmt.Fprintf(b, `<text class="t1" x="%d" y="%d" font-size="13" font-weight="600">%s</text>`, x, y+13, p.title)
	// 8.7 is the advance of the title's face at 13px semibold, wide enough for
	// the capitals in "Unmarshal"; the subtitle sits after it. It ends by
	// naming what the ratio is against, because the ratio itself has no room
	// to say it and the indented row only implies it.
	sub := fmt.Sprintf("%s · %s", p.sub, p.unit)
	if baseline != "" {
		sub += " · odjson vs " + baseline
	}
	fmt.Fprintf(b, `<text class="t2" x="%d" y="%d" font-size="11">%s</text>`,
		x+int(float64(len(p.title))*8.7)+8, y+13, esc(sub))
	if ratioInHeader {
		fmt.Fprintf(b, `<text x="%d" y="%d" font-size="16" font-weight="700" fill="%s" text-anchor="end">%s</text>`,
			x+panelW, y+14, t.accent, ratio)
	}
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="1" fill="%s"/>`, x, y+21, panelW, t.rule)

	for i, r := range p.rows {
		top := y + titleH + i*rowH
		labelX := x
		if r.indent {
			labelX += 10
		}
		ink := "t2"
		if r.emphasis {
			ink = "t1"
		}
		fmt.Fprintf(b, `<text class="mono %s" x="%d" y="%d" font-size="11">%s</text>`,
			ink, labelX, top+barH-5, esc(r.label))

		w := barW(r.value, max)
		fill := t.neutral
		if r.odjson {
			fill = t.accent
		}
		// Square against the axis, 4px rounded at the data end.
		fmt.Fprintf(b, `<path d="M%d %d h%d a%d %d 0 0 1 %d %d v%d a%d %d 0 0 1 %d %d h%d z" fill="%s"/>`,
			barX, top, w-radius, radius, radius, radius, radius, barH-2*radius, radius, radius, -radius, radius, -(w - radius), fill)

		val := fmtVal(r.value)
		valX := barX + w + gutter
		fmt.Fprintf(b, `<text class="mono %s" x="%d" y="%d" font-size="11">%s</text>`,
			ink, valX, top+barH-5, val)

		// Centred on the bar like every other row label — which is what the
		// -5 above is, at 11px — rather than sharing their baseline, which at
		// 16px would ride high. No t1/t2 class, because a stylesheet fill
		// beats a presentation attribute.
		if r.odjson && !ratioInHeader {
			// Between the two bars, a tick-ended rule across the length the
			// baseline has and odjson does not. It ends under the bar it is
			// measuring against, so the pair the ratio is about is drawn
			// rather than left to be inferred from the indent.
			if x0, x1 := barX+w, barX+baseW; x1-x0 >= 3*gutter {
				// 2px on whole coordinates, which is where an even stroke
				// lands on the pixel grid rather than across two.
				mid := top - (rowH-barH)/2
				fmt.Fprintf(b, `<path d="M%d %d v6 m0 -3 H%d m0 -3 v6" stroke="%s" stroke-width="2" fill="none"/>`,
					x0, mid-3, x1, t.accent)
			}
			fmt.Fprintf(b, `<text x="%d" y="%d" font-size="16" font-weight="700" fill="%s">%s</text>`,
				ratioX, top+barH/2+capH(16), t.accent, ratio)
		}
	}
}

// barW is a value's bar, in user units, on a scale whose longest bar is hi.
// Nothing is drawn narrower than its two rounded corners.
func barW(v, hi float64) int {
	return max(int(float64(barsW)*v/hi+0.5), 2*radius)
}

// capH is half the cap height of the sans face at the given size: the drop
// from a vertical centre to the baseline that sits text on it. At 11px it is
// 4, so barH/2+capH(11) is the barH-5 the row labels already use.
func capH(size int) int {
	return (size*36 + 50) / 100
}

// textW estimates the width of bold sans text at the given size, in user
// units. Letters and digits carry most of it; the rest are narrow.
func textW(s string, size float64) int {
	w := 0.0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			w += 0.58
		case r == '.', r == ' ':
			w += 0.29
		default:
			w += 0.6
		}
	}
	return int(w*size + 0.5)
}

func fmtVal(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

func altText() string {
	var parts []string
	for _, p := range panels {
		var vals []string
		for _, r := range p.rows {
			vals = append(vals, fmt.Sprintf("%s %s %s", r.label, fmtVal(r.value), p.unit))
		}
		parts = append(parts, fmt.Sprintf("%s %s: %s", p.title, p.sub, strings.Join(vals, ", ")))
	}
	return "Benchmark comparison, lower is better. " + strings.Join(parts, ". ") + "."
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// A key identifies one measurement in `go test -bench` output: the bench
// sub-module it was run in, plus the three parts of the benchmark's name.
type key struct {
	module, op, codec, payload string
}

// parse collects every ns/op in the input, keyed by measurement. A key can
// hold several samples, because `-count N` repeats each benchmark.
func parse(r io.Reader) (map[key][]float64, error) {
	out := make(map[key][]float64)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	module := ""
	for sc.Scan() {
		line := sc.Text()
		// `go test` prints one of these per package, ahead of its results.
		if rest, ok := strings.CutPrefix(line, "pkg:"); ok {
			module = path.Base(strings.TrimSpace(rest))
			continue
		}
		name, ns, ok := result(line)
		if !ok {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) != 3 {
			continue
		}
		k := key{module, strings.TrimPrefix(parts[0], "Benchmark"), parts[1], parts[2]}
		out[k] = append(out[k], ns)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read benchmark output: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("no benchmark results in the input")
	}
	return out, nil
}

// result pulls the name and the ns/op out of one testing.B result line,
// dropping the -N parallelism suffix that testing appends to every name.
func result(line string) (string, float64, bool) {
	f := strings.Fields(line)
	if len(f) < 4 || !strings.HasPrefix(f[0], "Benchmark") {
		return "", 0, false
	}
	i := slices.Index(f, "ns/op")
	if i < 2 {
		return "", 0, false
	}
	ns, err := strconv.ParseFloat(f[i-1], 64)
	if err != nil {
		return "", 0, false
	}

	name := f[0]
	if j := strings.LastIndex(name, "-"); j > 0 {
		if _, err := strconv.Atoi(name[j+1:]); err == nil {
			name = name[:j]
		}
	}
	return name, ns, true
}

// apply overwrites the panels' literals with the measured medians and
// recomputes each panel's ratio. It fails rather than drawing a partial
// chart: a missing bar reads as a measurement, not as an absence.
func apply(m map[key][]float64) error {
	for i := range panels {
		p := &panels[i]

		ns := make([]float64, len(p.rows))
		for j := range p.rows {
			r := &p.rows[j]
			v, ok := median(m[key{r.module, p.title, r.codec, p.payload}])
			if !ok {
				return fmt.Errorf("no Benchmark%s/%s/%s in bench/%s", p.title, r.codec, p.payload, r.module)
			}
			ns[j] = v
			// The panels' scales are the README's: µs for twitter, ns for small.
			if p.unit == "µs" {
				v /= 1000
			}
			r.value = v
		}

		// The odjson row is drawn indented under the baseline it improves on,
		// so the row above it is what the ratio is against.
		odjson := slices.IndexFunc(p.rows, func(r row) bool { return r.odjson })
		if odjson < 1 || !p.rows[odjson].indent {
			return fmt.Errorf("panel %s/%s has no odjson row under a baseline", p.title, p.payload)
		}
		p.ratio = strconv.FormatFloat(ns[odjson-1]/ns[odjson], 'f', 2, 64)
	}
	return nil
}

// median is the middle sample, which is what the README quotes: a mean would
// let one descheduled run on a shared CI machine move the bar.
func median(v []float64) (float64, bool) {
	if len(v) == 0 {
		return 0, false
	}
	s := slices.Sorted(slices.Values(v))
	n := len(s)
	if n%2 == 1 {
		return s[n/2], true
	}
	return (s[n/2-1] + s[n/2]) / 2, true
}
