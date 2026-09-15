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
//
// -chart picks which chart to draw. "readme" is the one above, and the
// default, so CI's invocation is unchanged. "direct" draws odjson's -direct
// functions against the standard entry points on the same types, from
// bench/direct, into docs/assets/direct-{light,dark}.svg:
//
//	go run ./chart -chart direct
//
// Its -input wants one row per process, concatenated — see bench/README.md's
// `direct` section. A single `go test -bench .` file renders too, but the
// odjson-direct rows come last in it and carry about 5% of ordering artefact
// on the encode side.
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
	label string
	value float64
	// indent draws the row as a variant of the one above — "encoding/json/v2"
	// and "+ odjson" under it are one library with and without odjson. A row
	// that is an alternative to the one above rather than an addition to it
	// (the -direct chart's) is not indented.
	indent   bool
	odjson   bool // the highlighted bar
	emphasis bool // label in primary ink rather than secondary

	// Where -input takes this row's number from: the bench sub-module
	// (`plain` measures the libraries as they ship, `gen` measures them on
	// odjson-generated types) and the codec label the benchmark uses.
	module, codec string
}

// A panel is one benchmark: the libraries on a scale of their own. The
// panels are drawn in two columns, Marshal on the left and Unmarshal on the
// right, so the slice lists every Marshal panel and then every Unmarshal one.
// The two sides do not carry the same rows — jettison only encodes and
// simdjson-go only decodes — so a panel's height is its own, and the two
// panels that share a grid row take the taller one's.
type panel struct {
	title string
	sub   string
	unit  string
	ratio string // odjson against encoding/json/v2, as the README's tables state it
	// verdict replaces the ratio when benchstat resolved no difference
	// between the two rows. "1.01x faster" would be a claim the measurement
	// does not make, so the panel carries benchstat's own token instead, and
	// it is drawn in secondary ink rather than the accent. -input clears it:
	// medians alone cannot say whether a difference is significant.
	verdict string
	rows    []row

	// Which benchmark -input reads for this panel, matching the names in
	// `go test -bench` output: BenchmarkMarshal/<codec>/<payload>.
	payload string
}

// A chartDef is one complete chart: its panels, the words around them and the
// base name of the files it is written to. The renderer below draws any of
// them; -chart picks which.
type chartDef struct {
	// id is what -chart takes; file is the stem of what it writes,
	// <file>-light.svg and <file>-dark.svg. They differ for the README's
	// chart, whose files the README and the image branch already reference
	// as bench-*.svg.
	id, file string
	// heading and sub are the two lines above the panels.
	heading string
	sub     string
	// vs opens the phrase naming what the highlighted row is measured
	// against, in each panel's subtitle and in the Markdown summary.
	vs string
	// footer is the default caption, naming where the numbers come from.
	footer string
	panels []panel
}

// The rows are in one fixed order: the baseline and odjson under it, then the
// reflection libraries, then the two other code generators. easyjson's row is
// its generated code and gojay's is hand-written against its API (its
// generator rejects interface{} fields); simdjson-go's is a parse plus a
// hand-written walk of its tape into the struct, so that it does the same job
// as the other Unmarshal rows. See bench/README.md.
var readmeChart = chartDef{
	id:      "readme",
	file:    "bench",
	heading: "encoding/json/v2, with and without odjson",
	sub:     "Time per operation — lower is better. Each panel has its own scale. The other libraries are baselines, as they ship.",
	vs:      "odjson vs",
	footer:  "Medians of 10 runs · AMD Ryzen 9 7950X · Linux · Go 1.27.1 · bench/plain, gen, easyjson and gojay",
	panels: []panel{
		{
			title: "Marshal", sub: "large · 616 KiB", unit: "µs", ratio: "4.22", payload: "twitter",
			rows: []row{
				{label: "encoding/json/v2", value: 401, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 95, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 117, module: "plain", codec: "sonic"},
				{label: "go-json", value: 251, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 405, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 217, module: "plain", codec: "segmentio"},
				{label: "jettison", value: 298, module: "plain", codec: "jettison"},
				{label: "easyjson (codegen)", value: 433, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 395, module: "gojay", codec: "gojay"},
			},
		},
		{
			title: "Marshal", sub: "medium · 13 KiB", unit: "ns", ratio: "4.39", payload: "medium",
			rows: []row{
				{label: "encoding/json/v2", value: 12259, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 2790, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 3606, module: "plain", codec: "sonic"},
				{label: "go-json", value: 4571, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 9011, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 4721, module: "plain", codec: "segmentio"},
				{label: "jettison", value: 7817, module: "plain", codec: "jettison"},
				{label: "easyjson (codegen)", value: 9787, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 17719, module: "gojay", codec: "gojay"},
			},
		},
		{
			title: "Marshal", sub: "small · 340 B", unit: "ns", ratio: "4.05", payload: "small",
			rows: []row{
				{label: "encoding/json/v2", value: 1033, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 255, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 323, module: "plain", codec: "sonic"},
				{label: "go-json", value: 402, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 537, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 379, module: "plain", codec: "segmentio"},
				{label: "jettison", value: 464, module: "plain", codec: "jettison"},
				{label: "easyjson (codegen)", value: 660, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 636, module: "gojay", codec: "gojay"},
			},
		},
		{
			title: "Unmarshal", sub: "large · 616 KiB", unit: "µs", ratio: "2.93", payload: "twitter",
			rows: []row{
				{label: "encoding/json/v2", value: 1100, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 376, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 505, module: "plain", codec: "sonic"},
				{label: "go-json", value: 662, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 1064, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 837, module: "plain", codec: "segmentio"},
				{label: "simdjson-go", value: 607, module: "plain", codec: "simdjson-go"},
				{label: "easyjson (codegen)", value: 1086, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 2570, module: "gojay", codec: "gojay"},
			},
		},
		{
			title: "Unmarshal", sub: "medium · 13 KiB", unit: "ns", ratio: "2.69", payload: "medium",
			rows: []row{
				{label: "encoding/json/v2", value: 22222, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 8263, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 13941, module: "plain", codec: "sonic"},
				{label: "go-json", value: 14991, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 21630, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 16306, module: "plain", codec: "segmentio"},
				{label: "simdjson-go", value: 30207, module: "plain", codec: "simdjson-go"},
				{label: "easyjson (codegen)", value: 18376, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 25770, module: "gojay", codec: "gojay"},
			},
		},
		{
			title: "Unmarshal", sub: "small · 340 B", unit: "ns", ratio: "3.39", payload: "small",
			rows: []row{
				{label: "encoding/json/v2", value: 1853, module: "plain", codec: "json-v2"},
				{label: "+ odjson", value: 547, indent: true, odjson: true, emphasis: true, module: "gen", codec: "json-v2"},
				{label: "sonic", value: 964, module: "plain", codec: "sonic"},
				{label: "go-json", value: 788, module: "plain", codec: "go-json"},
				{label: "json-iterator", value: 1113, module: "plain", codec: "json-iterator"},
				{label: "segmentio/encoding", value: 1108, module: "plain", codec: "segmentio"},
				{label: "simdjson-go", value: 1572, module: "plain", codec: "simdjson-go"},
				{label: "easyjson (codegen)", value: 1158, module: "easyjson", codec: "easyjson"},
				{label: "gojay (hand-written)", value: 1069, module: "gojay", codec: "gojay"},
			},
		},
	},
}

// directChart answers a different question from the one above, which is why
// it is a chart of its own rather than three more rows: not where odjson sits
// among the host libraries, but what odjson's own -direct functions are worth
// against its own standard entry points.
//
// Every bar here runs the identical generated codec. The baseline row is a
// plain json.Marshal / json.Unmarshal from encoding/json/v2 reaching the
// generated methods; the accented row is the package level function -direct
// adds. Only the call differs — and bench/direct is generated with
// -escape-html=false so that the bytes do not, which TestDirectMatchesJSONV2
// holds it to.
//
// The -direct row is not indented under the baseline, the way "+ odjson" is
// in the chart above. That indent means "the row above, plus this"; MarshalT
// is not json/v2 plus anything, it is the other way in. The two rows are
// alternatives, and the chart draws them as alternatives.
//
// AppendT is measured (see BenchmarkAppendDirect and docs/internals.md) but
// not drawn: it writes into a buffer the caller keeps, so it does not do the
// job the other two rows do, and a bar half the length of one that allocates
// invites the comparison anyway.
var directChart = chartDef{
	id:      "direct",
	file:    "direct",
	heading: "odjson's -direct functions, against its own standard entry points",
	sub:     "Lower is better. Each panel: the standard call reaching the generated codec, and the -direct function under it — same codec, same bytes.",
	vs:      "vs",
	footer:  "One row per process, interleaved · 6 runs · benchstat medians · AMD Ryzen 9 7950X · Linux · Go 1.27.1 · bench/direct",
	panels: []panel{
		{
			title: "Marshal", sub: "large · 616 KiB", unit: "µs", ratio: "1.01", verdict: "~ p=0.394", payload: "twitter",
			rows: []row{
				{label: "json/v2 + odjson", value: 106.3, module: "direct", codec: "json-v2"},
				{label: "MarshalT", value: 104.9, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
		{
			title: "Marshal", sub: "medium · 13 KiB", unit: "ns", ratio: "1.06", payload: "medium",
			rows: []row{
				{label: "json/v2 + odjson", value: 2924, module: "direct", codec: "json-v2"},
				{label: "MarshalT", value: 2749, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
		{
			title: "Marshal", sub: "small · 365 B", unit: "ns", ratio: "1.32", payload: "small",
			rows: []row{
				{label: "json/v2 + odjson", value: 275.2, module: "direct", codec: "json-v2"},
				{label: "MarshalT", value: 208.7, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
		{
			title: "Unmarshal", sub: "large · 616 KiB", unit: "µs", ratio: "1.01", verdict: "~ p=0.240", payload: "twitter",
			rows: []row{
				{label: "json/v2 + odjson", value: 400.9, module: "direct", codec: "json-v2"},
				{label: "UnmarshalT", value: 396.9, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
		{
			title: "Unmarshal", sub: "medium · 13 KiB", unit: "ns", ratio: "1.02", payload: "medium",
			rows: []row{
				{label: "json/v2 + odjson", value: 8914, module: "direct", codec: "json-v2"},
				{label: "UnmarshalT", value: 8722, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
		{
			title: "Unmarshal", sub: "small · 365 B", unit: "ns", ratio: "1.27", payload: "small",
			rows: []row{
				{label: "json/v2 + odjson", value: 596.4, module: "direct", codec: "json-v2"},
				{label: "UnmarshalT", value: 471.0, odjson: true, emphasis: true, module: "direct", codec: "odjson-direct"},
			},
		},
	},
}

var charts = []*chartDef{&readmeChart, &directChart}

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

	labelW = 150 // library names: the longest, "gojay (hand-written)", is 20 characters of 11px mono
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

func main() {
	which := flag.String("chart", "readme", "which chart to draw: readme or direct")
	input := flag.String("input", "", "`go test -bench` output to take the numbers from; the chart's own literals are used when empty")
	out := flag.String("out", "", "directory to write <chart>-light.svg and <chart>-dark.svg into (default docs/assets)")
	footer := flag.String("footer", "", "the caption under the chart, naming where the numbers come from; the chart's own is used when empty")
	summary := flag.String("summary", "", "also write the same numbers to this file as a Markdown table")
	flag.Parse()

	def := pick(*which)
	if def == nil {
		fail(fmt.Errorf("unknown -chart %q", *which))
	}
	caption := *footer
	if caption == "" {
		caption = def.footer
	}

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
		if err := apply(def, measured); err != nil {
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
		name := filepath.Join(dir, def.file+"-"+t.name+".svg")
		if err := os.WriteFile(name, render(def, t, caption), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", name)
	}

	if *summary != "" {
		if err := os.WriteFile(*summary, table(def, caption), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", *summary)
	}
}

// pick resolves -chart to one of the definitions above.
func pick(id string) *chartDef {
	for _, c := range charts {
		if c.id == id {
			return c
		}
	}
	return nil
}

// table restates the chart as Markdown. A PNG of the chart carries no alt
// text, so wherever the image goes this goes with it. The columns are every
// label any panel carries, in first-seen order, with an em dash where a panel
// has no such row: jettison only encodes and simdjson-go only decodes.
func table(def *chartDef, footer string) []byte {
	var labels []string
	for _, p := range def.panels {
		for _, r := range p.rows {
			if !slices.Contains(labels, r.label) {
				labels = append(labels, r.label)
			}
		}
	}

	var b bytes.Buffer
	b.WriteString("| Benchmark |")
	for _, l := range labels {
		fmt.Fprintf(&b, " %s |", l)
	}
	b.WriteString("\n| --- |")
	for range labels {
		b.WriteString(" ---: |")
	}
	b.WriteByte('\n')

	for _, p := range def.panels {
		fmt.Fprintf(&b, "| %s · %s |", p.title, p.sub)
		for _, l := range labels {
			i := slices.IndexFunc(p.rows, func(r row) bool { return r.label == l })
			if i < 0 {
				b.WriteString(" — |")
				continue
			}
			r := p.rows[i]
			switch {
			case r.odjson && p.verdict != "":
				fmt.Fprintf(&b, " **%s %s** (%s) |", fmtVal(r.value), p.unit, p.verdict)
			case r.odjson:
				fmt.Fprintf(&b, " **%s %s** (%s×) |", fmtVal(r.value), p.unit, p.ratio)
			default:
				fmt.Fprintf(&b, " %s %s |", fmtVal(r.value), p.unit)
			}
		}
		b.WriteByte('\n')
	}

	// The same sentence the panels carry, for the same reason: a bare ratio
	// does not say what it is against.
	if i := slices.IndexFunc(def.panels[0].rows, func(r row) bool { return r.odjson }); i > 0 {
		fmt.Fprintf(&b, "\n× is %s %s.\n", def.vs, def.panels[0].rows[i-1].label)
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

// perColumn is how many panels each of the two columns holds: the Marshal
// panels down the left, the Unmarshal panels down the right.
func perColumn(def *chartDef) int {
	return (len(def.panels) + 1) / 2
}

// gridRows lays the panels out: the height of each grid row, which is the
// taller of the two panels sharing it, and the y offset each starts at.
func gridRows(def *chartDef) (heights, tops []int) {
	rows := perColumn(def)
	heights = make([]int, rows)
	tops = make([]int, rows)
	for i, p := range def.panels {
		r := i % rows
		heights[r] = max(heights[r], titleH+rowH*len(p.rows))
	}
	y := headerH
	for r := range rows {
		tops[r] = y
		y += heights[r] + panelGapY
	}
	return heights, tops
}

func render(def *chartDef, t theme, footer string) []byte {
	heights, tops := gridRows(def)
	rows := perColumn(def)
	svgH := tops[rows-1] + heights[rows-1] + footerH

	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s">`,
		svgW, svgH, svgW, svgH, esc(altText(def)))
	fmt.Fprintf(&b, `<style>
text{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace}
.t1{fill:%s}.t2{fill:%s}
</style>`, t.primary, t.secondary)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`, svgW, svgH, t.surface)

	fmt.Fprintf(&b, `<text class="t1" x="%d" y="26" font-size="16" font-weight="600">%s</text>`, padX, esc(def.heading))
	fmt.Fprintf(&b, `<text class="t2" x="%d" y="46" font-size="12">%s</text>`, padX, esc(def.sub))

	showVS := vsFits(def, t)
	for i, p := range def.panels {
		x := padX + (i/rows)*(panelW+panelGapX)
		y := tops[i%rows]
		drawPanel(&b, def, t, p, x, y, showVS)
	}

	fmt.Fprintf(&b, `<text class="t2" x="%d" y="%d" font-size="11">%s</text>`,
		padX, svgH-11, esc(footer))
	b.WriteString(`</svg>`)
	b.WriteByte('\n')
	return b.Bytes()
}

// vsFits reports whether every panel has room on its title line for the
// clause naming what its ratio is against. The answer is one per chart, not
// one per panel: in a grid of small multiples a clause that comes and goes
// reads as an inconsistency, so either all of them carry it or none do.
//
// It only ever has to give way when the ratio is in the header, which is
// where a ratio near 1 goes — the README's chart, whose ratios are all around
// 4, draws them beside the bars and keeps the clause unconditionally.
func vsFits(def *chartDef, t theme) bool {
	for _, p := range def.panels {
		max := 0.0
		for _, r := range p.rows {
			if r.value > max {
				max = r.value
			}
		}
		i := slices.IndexFunc(p.rows, func(r row) bool { return r.odjson })
		if i < 1 {
			continue
		}
		ratio, _ := ratioText(p, t)
		// Relative to the panel's own left edge: the inline position and the
		// right edge both move with x, so where the panel sits does not
		// change the answer.
		dx := labelW + gutter + barW(p.rows[i].value, max) + gutter + len(fmtVal(p.rows[i].value))*7 + 10
		if dx+textW(ratio, 16) <= panelW {
			continue // drawn beside the bar; the title line is free
		}
		sub := fmt.Sprintf("%s · %s · %s %s", p.sub, p.unit, def.vs, p.rows[i-1].label)
		subX := int(float64(len(p.title))*8.7) + 8
		if subX+textW(sub, 11)+gutter > panelW-textW(ratio, 16) {
			return false
		}
	}
	return true
}

// ratioText is a panel's headline and the ink it is drawn in: the measured
// margin in the accent, or benchstat's own token in secondary ink when the
// run resolved no difference, because "1.01× faster" would claim one.
func ratioText(p panel, t theme) (string, string) {
	if p.verdict != "" {
		return p.verdict, t.secondary
	}
	return p.ratio + "× faster", t.accent
}

func drawPanel(b *bytes.Buffer, def *chartDef, t theme, p panel, x, y int, showVS bool) {
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
	ratio, ratioInk := ratioText(p, t)
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
	if showVS && baseline != "" {
		sub += " · " + def.vs + " " + baseline
	}
	fmt.Fprintf(b, `<text class="t2" x="%d" y="%d" font-size="11">%s</text>`,
		x+int(float64(len(p.title))*8.7)+8, y+13, esc(sub))
	if ratioInHeader {
		fmt.Fprintf(b, `<text x="%d" y="%d" font-size="16" font-weight="700" fill="%s" text-anchor="end">%s</text>`,
			x+panelW, y+14, ratioInk, ratio)
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
				ratioX, top+barH/2+capH(16), ratioInk, ratio)
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

func altText(def *chartDef) string {
	var parts []string
	for _, p := range def.panels {
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
	// module is the bench sub-module; name is the benchmark's own name with
	// "Benchmark" and the -N parallelism suffix stripped, so a row that does
	// not follow the three-part shape can name itself (see row.bench).
	module, name string
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
		k := key{module, strings.TrimPrefix(name, "Benchmark")}
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
func apply(def *chartDef, m map[key][]float64) error {
	for i := range def.panels {
		p := &def.panels[i]

		ns := make([]float64, len(p.rows))
		for j := range p.rows {
			r := &p.rows[j]
			name := p.title + "/" + r.codec + "/" + p.payload
			v, ok := median(m[key{r.module, name}])
			if !ok {
				return fmt.Errorf("no Benchmark%s in bench/%s", name, r.module)
			}
			ns[j] = v
			// The panels' scales are the README's: µs for twitter, ns for
			// medium and small, whose values print whole in that unit.
			if p.unit == "µs" {
				v /= 1000
			}
			r.value = v
		}

		// The highlighted row is drawn directly under the baseline it is
		// measured against, indented or not, so the row above it is what the
		// ratio is against.
		odjson := slices.IndexFunc(p.rows, func(r row) bool { return r.odjson })
		if odjson < 1 {
			return fmt.Errorf("panel %s/%s has no odjson row under a baseline", p.title, p.payload)
		}
		p.ratio = strconv.FormatFloat(ns[odjson-1]/ns[odjson], 'f', 2, 64)
		// Whatever a literal panel recorded about significance belongs to the
		// run it was measured in. Medians of someone else's run cannot say,
		// so the ratio stands on its own.
		p.verdict = ""
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
