package plain

import (
	jsonv1 "encoding/json"
	"testing"
)

// EXPERIMENT ONLY (not for merge).
//
// BenchmarkUnmarshalVaried answers one question: how much of the decode
// numbers comes from the benchmark handing every library the same document
// over and over, so that a string cache that survives between calls is warm
// from the first member on?
//
// Both odjson's StringCache and json/v2's own [256]string table live on a
// pooled object that is not cleared between calls, so a repeated document
// leaves both warm. sonic and go-json have no such table.
//
// Each row is run twice over a ring of documents:
//
//   - "same": identical copies of the fixture. The ring's memory footprint,
//     the indexing and the cache pressure are the control.
//   - "varied": documents whose string *values* differ, letter rotated, the
//     same length, the same member names, the same types.
//
// The difference between the two is content distinctness and nothing else.
// The ring is sized so that the distinct string values across it comfortably
// exceed the 256 slots both tables have, which is what a workload of
// unrelated documents looks like to them.

// ringTarget is how many distinct string values the varied ring aims to hold.
const ringTarget = 1024

// countValueStrings reports how many distinct string values (not member
// names) the document holds, which is what sizes the ring.
func countValueStrings(data []byte) int {
	seen := make(map[string]struct{})
	forEachValueString(data, func(body []byte) {
		seen[string(body)] = struct{}{}
	})
	return max(len(seen), 1)
}

// forEachValueString calls fn with the body of every string literal that is
// not a member name.
func forEachValueString(data []byte, fn func(body []byte)) {
	for i := 0; i < len(data); {
		if data[i] != '"' {
			i++
			continue
		}
		start := i + 1
		j := start
		for j < len(data) && data[j] != '"' {
			if data[j] == '\\' {
				if j+1 < len(data) && data[j+1] == 'u' {
					j += 6 // \uXXXX: the hex digits must stay hex
					continue
				}
				j += 2
				continue
			}
			j++
		}
		if j >= len(data) {
			return
		}
		end := j

		// A literal followed by a colon is a member name: leave it.
		k := j + 1
		for k < len(data) && (data[k] == ' ' || data[k] == '\t' || data[k] == '\n' || data[k] == '\r') {
			k++
		}
		if k >= len(data) || data[k] != ':' {
			fn(data[start:end])
		}
		i = end + 1
	}
}

// rotateValues returns data with every ASCII letter inside a string *value*
// advanced by n places, leaving member names, numbers, literals, escapes and
// non-ASCII bytes alone. The length is preserved, so the two rings have the
// same footprint and the same shape.
func rotateValues(data []byte, n int) []byte {
	out := make([]byte, len(data))
	copy(out, data)

	rot := func(c byte) byte {
		switch {
		case c >= 'a' && c <= 'z':
			return 'a' + (c-'a'+byte(n))%26
		case c >= 'A' && c <= 'Z':
			return 'A' + (c-'A'+byte(n))%26
		}
		return c
	}

	base := out
	forEachValueString(base, func(body []byte) {
		for m := 0; m < len(body); {
			if body[m] == '\\' {
				if m+1 < len(body) && body[m+1] == 'u' {
					m += 6
					continue
				}
				m += 2
				continue
			}
			if body[m] < 0x80 {
				body[m] = rot(body[m])
			}
			m++
		}
	})
	return out
}

func BenchmarkUnmarshalVaried(b *testing.B) {
	ps := payloads(b)

	for _, p := range ps {
		// Size the ring so the distinct string values across it exceed the
		// 256 slots both string tables have, several times over.
		distinct := countValueStrings(p.data)
		ring := min(max((ringTarget+distinct-1)/distinct, 2), 128)
		b.Logf("%s: %d distinct string values, ring of %d", p.name, distinct, ring)

		varied := make([][]byte, ring)
		same := make([][]byte, ring)
		for i := range ring {
			varied[i] = rotateValues(p.data, i+1)
			same[i] = append([]byte(nil), p.data...)
		}

		// A rotated document must still decode into the same type, or the
		// row would be measuring an error path.
		if err := jsonv1.Unmarshal(varied[0], p.newValue()); err != nil {
			b.Fatalf("%s: rotated variant does not decode: %v", p.name, err)
		}
		if len(varied[0]) != len(p.data) {
			b.Fatalf("%s: rotation changed the length", p.name)
		}

		for _, c := range codecs {
			switch c.name {
			case "encoding-json", "json-v2", "sonic", "go-json":
			default:
				continue
			}

			for _, r := range []struct {
				name string
				bufs [][]byte
			}{{"same", same}, {"varied", varied}} {
				b.Run(c.name+"/"+p.name+"/"+r.name, func(b *testing.B) {
					if err := c.unmarshal(r.bufs[0], p.newValue()); err != nil {
						b.Fatal(err)
					}

					b.ReportAllocs()
					b.SetBytes(int64(len(p.data)))

					i := 0
					for b.Loop() {
						if err := c.unmarshal(r.bufs[i], p.newValue()); err != nil {
							b.Fatal(err)
						}
						i++
						if i == len(r.bufs) {
							i = 0
						}
					}
				})
			}
		}
	}
}
