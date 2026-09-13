package odjsonrt

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// TestAtofPow10 recomputes the table with math/big: each entry must be
// ⌊10^q · 2^k⌋ for the k that puts it in [2^127, 2^128), stored as
// hi·2^64 + lo.
func TestAtofPow10(t *testing.T) {
	if n := atofPow10Max - atofPow10Min + 1; len(atofPow10) != n {
		t.Fatalf("table has %d entries, want %d", len(atofPow10), n)
	}
	one := big.NewInt(1)
	two := big.NewRat(2, 1)
	lo128 := new(big.Rat).SetInt(new(big.Int).Lsh(one, 127))
	hi128 := new(big.Rat).SetInt(new(big.Int).Lsh(one, 128))
	for q := atofPow10Min; q <= atofPow10Max; q++ {
		r := new(big.Rat)
		if q >= 0 {
			r.SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(q)), nil))
		} else {
			r.SetFrac(one, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-q)), nil))
		}
		for r.Cmp(lo128) < 0 {
			r.Mul(r, two)
		}
		for r.Cmp(hi128) >= 0 {
			r.Quo(r, two)
		}
		want := new(big.Int).Quo(r.Num(), r.Denom())
		e := atofPow10[q-atofPow10Min]
		got := new(big.Int).Lsh(new(big.Int).SetUint64(e.hi), 64)
		got.Add(got, new(big.Int).SetUint64(e.lo))
		if got.Cmp(want) != 0 {
			t.Errorf("1e%d: table holds %v, want %v", q, got, want)
		}
	}
}

// checkSimpleFloat parses lit through the fast path and holds whatever it
// accepts to strconv.ParseFloat's bits and end offset; declining is always
// allowed. It reports whether the literal was accepted.
func checkSimpleFloat(t *testing.T, lit string, bitSize int) bool {
	t.Helper()
	data := []byte(lit + "]")
	got, end, ok := ParseSimpleFloat(data, 0, bitSize)
	if !ok {
		if end != 0 {
			t.Errorf("%q: declined with end = %d", lit, end)
		}
		return false
	}
	want, err := strconv.ParseFloat(lit, bitSize)
	if err != nil {
		t.Errorf("%q: accepted, strconv: %v", lit, err)
		return true
	}
	if end != len(lit) {
		t.Errorf("%q: end = %d, want %d", lit, end, len(lit))
	}
	if math.Float64bits(got) != math.Float64bits(want) {
		t.Errorf("%q (%d bits): got %v (%#x), want %v (%#x)", lit, bitSize, got, math.Float64bits(got), want, math.Float64bits(want))
	}
	return true
}

// TestParseSimpleFloatHardCases runs the literals that decide a float
// parser: exact halfway points, the digits around 2^53, the ends of the
// float64 range, the sign of zero, and the exponent forms.
func TestParseSimpleFloatHardCases(t *testing.T) {
	accepted := []string{
		// Full precision coordinates, the shape of canada.json.
		"-65.613616999999977", "43.420273000000009", "-65.625", "43.421379000000059",
		"-129.32812499999997", "50.083322000000112", "179.99999999999997",
		// Around 2^53, where the mantissa stops being exact.
		"9007199254740992", "9007199254740993", "9007199254740994", "9007199254740995",
		"18014398509481984", "18014398509481985", "18014398509481986",
		// Exponents, both cases, both signs, leading zeros in the exponent.
		"1e5", "1E5", "1.5e-3", "1e+5", "1e-5", "1e0", "1e-0", "1e00", "1e-00", "2.5E+007",
		"1e23", "1e22", "8.5e22", "1e-22", "1e-23", "123456789012345678e-100", "1e300", "1e-300",
		"1.7976931348623157e308", "4.9406564584124654e-324", "2.2250738585072014e-308",
		"1e400", "1e-400", "0e400", "0.0e-400",
		// Zero, either sign, however spelled.
		"0", "-0", "0.0", "-0.0", "0.000", "-0.000000000000000000",
		// Nineteen digits: the most the mantissa word holds.
		"1234567890123456789", "9999999999999999999", "0.999999999999999999",
		"1.234567890123456789", "12345678901.23456789",
		// The Clinger boundary.
		"0.1", "0.3", "0.30000000000000004", "1e15", "1e16", "9007199254740991e22",
		// Small values, leading fraction zeros.
		"0.000001", "0.00000000000000000001", "123.000000000000001",
	}
	for _, lit := range accepted {
		checkSimpleFloat(t, lit, 64)
		checkSimpleFloat(t, lit, 32)
	}
	// Literals the general path must handle: strconv accepts them all.
	for _, lit := range []string{"1e400", "1e-400", "1e5", "4.9406564584124654e-324", "2.2250738585072011e-308"} {
		v, end, err := ParseFloat([]byte(lit), 0, 64)
		want, werr := strconv.ParseFloat(lit, 64)
		if (err != nil) != (werr != nil) {
			t.Errorf("ParseFloat(%q): err = %v, strconv %v", lit, err, werr)
			continue
		}
		if err == nil && (math.Float64bits(v) != math.Float64bits(want) || end != len(lit)) {
			t.Errorf("ParseFloat(%q) = %v/%d, want %v/%d", lit, v, end, want, len(lit))
		}
	}
	// A subnormal is declined, not rounded.
	for _, lit := range []string{"4.9406564584124654e-324", "2.2250738585072011e-308", "1e-310"} {
		if _, _, ok := ParseSimpleFloat([]byte(lit), 0, 64); ok {
			t.Errorf("%q: a subnormal was accepted", lit)
		}
	}
	declined := []string{
		"", "-", "+1", ".5", "1.", "01", "-01", "00", "-.5", "1e", "1e+", "1e-", "1.e5", "1.5e", "e5",
		"12345678901234567890",      // twenty digits
		"1.2345678901234567890",     // twenty digits with a point
		"0.00000000000000000000001", // twenty-three fraction digits
		"1234567890123456789.0",     // twenty digits either side
		"-12345678901234567890e-5",  // twenty digits and an exponent
		"Infinity", "NaN", "nan", "inf", "١",
	}
	for _, lit := range declined {
		if _, _, ok := ParseSimpleFloat([]byte(lit), 0, 64); ok {
			t.Errorf("%q: accepted", lit)
		}
	}
}

// TestParseSimpleFloatTerminators checks that the byte after the literal,
// whatever it is, neither joins the number nor changes its value: the word
// loads of the fraction read past its end.
func TestParseSimpleFloatTerminators(t *testing.T) {
	for _, lit := range []string{"-65.613616999999977", "1.5", "0.12345678", "0.123456789", "12.3456789012345678", "7", "1e5", "1.5e-3"} {
		want, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			t.Fatal(err)
		}
		for c := range 256 {
			if c >= '0' && c <= '9' {
				continue
			}
			if (c == 'e' || c == 'E') && !strings.ContainsAny(lit, "eE") || c == '.' && !strings.Contains(lit, ".") {
				// Would begin an exponent or a fraction: not a terminator.
				continue
			}
			data := []byte(lit)
			data = append(data, byte(c))
			got, end, ok := ParseSimpleFloat(data, 0, 64)
			if !ok {
				t.Errorf("%q + %#x: declined", lit, c)
				continue
			}
			if end != len(lit) || math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("%q + %#x: got %v/%d, want %v/%d", lit, c, got, end, want, len(lit))
			}
		}
		// And at the very end of the input, where no word can be loaded.
		got, end, ok := ParseSimpleFloat([]byte(lit), 0, 64)
		if !ok || end != len(lit) || math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("%q at end: got %v/%d/%v, want %v/%d", lit, got, end, ok, want, len(lit))
		}
	}
}

// TestParseSimpleFloatRandom sweeps mantissas of one to nineteen digits
// with the point at every position and exponents across the float64
// range, and holds every accepted literal to strconv's bits.
func TestParseSimpleFloatRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	n := 200000
	if testing.Short() {
		n = 20000
	}
	accepted, total := 0, 0
	for range n {
		nd := 1 + r.IntN(19)
		var sb strings.Builder
		if r.IntN(2) == 0 {
			sb.WriteByte('-')
		}
		digits := make([]byte, nd)
		for i := range digits {
			digits[i] = byte('0' + r.IntN(10))
		}
		if digits[0] == '0' && nd > 1 {
			digits[0] = '1'
		}
		point := r.IntN(nd + 1) // digits before the point; nd means none
		if point == 0 {
			sb.WriteString("0.")
			sb.Write(digits)
		} else {
			sb.Write(digits[:point])
			if point < nd {
				sb.WriteByte('.')
				sb.Write(digits[point:])
			}
		}
		switch r.IntN(4) {
		case 0:
			sb.WriteByte('e')
			sb.WriteString(strconv.Itoa(r.IntN(700) - 350))
		case 1:
			sb.WriteByte('E')
			sb.WriteString(strconv.Itoa(r.IntN(60) - 30))
		}
		lit := sb.String()
		total++
		if checkSimpleFloat(t, lit, 64) {
			accepted++
		}
		checkSimpleFloat(t, lit, 32)
	}
	if accepted < total*9/10 {
		t.Errorf("accepted %d of %d literals", accepted, total)
	}

	// Neighbours of halfway points: the decimal expansions of
	// m + ½ulp for random floats, to seventeen digits and beyond.
	for range n / 10 {
		f := math.Float64frombits(r.Uint64() &^ (1 << 63))
		if math.IsInf(f, 0) || math.IsNaN(f) || f == 0 {
			continue
		}
		next := math.Nextafter(f, math.Inf(1))
		for _, prec := range []int{15, 16, 17, 18} {
			checkSimpleFloat(t, strconv.FormatFloat(f, 'e', prec, 64), 64)
			checkSimpleFloat(t, strconv.FormatFloat(f, 'g', -1, 64), 64)
			mid := new(big.Float).SetPrec(200).Add(new(big.Float).SetPrec(200).SetFloat64(f), new(big.Float).SetPrec(200).SetFloat64(next))
			mid.Quo(mid, big.NewFloat(2))
			checkSimpleFloat(t, mid.Text('e', prec), 64)
		}
	}
}

// TestParseSimpleFloatCanada parses every number of a canada-shaped ring,
// in place in the document, against strconv.
func TestParseSimpleFloatCanada(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	var doc []byte
	var lits []string
	doc = append(doc, "[["...)
	for i := range 2000 {
		if i > 0 {
			doc = append(doc, ',')
		}
		x := strconv.FormatFloat(r.Float64()*360-180, 'f', -1, 64)
		y := strconv.FormatFloat(r.Float64()*180-90, 'f', -1, 64)
		lits = append(lits, x, y)
		doc = append(doc, '[')
		doc = append(doc, x...)
		doc = append(doc, ',')
		doc = append(doc, y...)
		doc = append(doc, ']')
	}
	doc = append(doc, "]]"...)
	p := 2
	for _, lit := range lits {
		for doc[p] == '[' || doc[p] == ',' || doc[p] == ']' {
			p++
		}
		got, end, ok := ParseSimpleFloat(doc, p, 64)
		want, _ := strconv.ParseFloat(lit, 64)
		if !ok || end != p+len(lit) || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("%q at %d: got %v/%d/%v, want %v/%d", lit, p, got, end, ok, want, p+len(lit))
		}
		p = end
	}
}

func BenchmarkParseSimpleFloat(b *testing.B) {
	for _, tc := range []struct{ name, lit string }{
		{"canada", "-65.613616999999977,"},
		{"short", "40.8,"},
		{"integer", "1234,"},
		{"exponent", "1.5e-3,"},
		{"nineteen", "1.234567890123456789,"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			data := []byte(tc.lit)
			for b.Loop() {
				if _, _, ok := ParseSimpleFloat(data, 0, 64); !ok {
					b.Fatal("declined")
				}
			}
		})
	}
	b.Run("strconv-canada", func(b *testing.B) {
		lit := "-65.613616999999977"
		for b.Loop() {
			if _, err := strconv.ParseFloat(lit, 64); err != nil {
				b.Fatal(err)
			}
		}
	})
}
