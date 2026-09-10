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
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// A row is one library's measurement within a panel, in the panel's unit.
type row struct {
	label    string
	value    float64
	indent   bool // drawn as a variant of the row above it
	odjson   bool // the highlighted bar
	emphasis bool // label in primary ink rather than secondary
}

// A panel is one benchmark: four libraries on a scale of their own.
type panel struct {
	title string
	sub   string
	unit  string
	ratio string // odjson against encoding/json/v2, as the README's tables state it
	rows  []row
}

var panels = []panel{
	{
		title: "Marshal", sub: "twitter · 616 KiB", unit: "µs", ratio: "3.53",
		rows: []row{
			{label: "encoding/json/v2", value: 392},
			{label: "+ odjson", value: 111, indent: true, odjson: true, emphasis: true},
			{label: "sonic", value: 117},
			{label: "go-json", value: 237},
		},
	},
	{
		title: "Marshal", sub: "small · 340 B", unit: "ns", ratio: "3.23",
		rows: []row{
			{label: "encoding/json/v2", value: 1025},
			{label: "+ odjson", value: 317, indent: true, odjson: true, emphasis: true},
			{label: "sonic", value: 308},
			{label: "go-json", value: 374},
		},
	},
	{
		title: "Unmarshal", sub: "twitter · 616 KiB", unit: "µs", ratio: "2.12",
		rows: []row{
			{label: "encoding/json/v2", value: 1072},
			{label: "+ odjson", value: 506, indent: true, odjson: true, emphasis: true},
			{label: "sonic", value: 492},
			{label: "go-json", value: 655},
		},
	},
	{
		title: "Unmarshal", sub: "small · 340 B", unit: "ns", ratio: "3.21",
		rows: []row{
			{label: "encoding/json/v2", value: 1842},
			{label: "+ odjson", value: 573, indent: true, odjson: true, emphasis: true},
			{label: "sonic", value: 977},
			{label: "go-json", value: 770},
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

func main() {
	// Anchored on this file rather than the working directory, so running it
	// from bench/chart writes the same place as running it from bench/.
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		fail(errors.New("cannot locate the source directory"))
	}
	out := filepath.Join(filepath.Dir(self), "..", "..", "docs", "assets")
	if _, err := os.Stat(out); err != nil {
		fail(err)
	}
	for _, t := range themes {
		name := filepath.Join(out, "bench-"+t.name+".svg")
		if err := os.WriteFile(name, render(t), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", name)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "chart:", err)
	os.Exit(1)
}

func render(t theme) []byte {
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

	fmt.Fprintf(&b, `<text class="t2" x="%d" y="%d" font-size="11">Medians of 10 runs · AMD Ryzen 9 7950X · Linux · Go 1.27.1 · bench/plain and bench/gen</text>`,
		padX, svgH-11)
	b.WriteString(`</svg>`)
	b.WriteByte('\n')
	return b.Bytes()
}

func drawPanel(b *bytes.Buffer, t theme, p panel, x, y int) {
	fmt.Fprintf(b, `<text class="t1" x="%d" y="%d" font-size="13" font-weight="600">%s</text>`, x, y+13, p.title)
	fmt.Fprintf(b, `<text class="t2" x="%d" y="%d" font-size="11">%s · %s · odjson %s× faster</text>`,
		x+len(p.title)*8+8, y+13, p.sub, p.unit, p.ratio)
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="1" fill="%s"/>`, x, y+21, panelW, t.rule)

	max := 0.0
	for _, r := range p.rows {
		if r.value > max {
			max = r.value
		}
	}

	barX := x + labelW + gutter
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

		w := int(float64(barsW)*r.value/max + 0.5)
		if w < 2*radius {
			w = 2 * radius
		}
		fill := t.neutral
		if r.odjson {
			fill = t.accent
		}
		// Square against the axis, 4px rounded at the data end.
		fmt.Fprintf(b, `<path d="M%d %d h%d a%d %d 0 0 1 %d %d v%d a%d %d 0 0 1 %d %d h%d z" fill="%s"/>`,
			barX, top, w-radius, radius, radius, radius, radius, barH-2*radius, radius, radius, -radius, radius, -(w - radius), fill)

		fmt.Fprintf(b, `<text class="mono %s" x="%d" y="%d" font-size="11">%s</text>`,
			ink, barX+w+gutter, top+barH-5, fmtVal(r.value))
	}
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
