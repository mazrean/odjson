package proto

import (
	"bytes"

	"github.com/mazrean/odjson/odjsonrt"
)

// trusted selects the string parser. jsontext validates the UTF-8 of every
// value it hands to UnmarshalJSONFrom, so a decoder reached that way can skip
// odjsonrt.ParseString's own utf8.Valid pass over every non-ASCII string.
type trusted bool

func (t trusted) str(data []byte, p int) (string, int, error) {
	if !t {
		return odjsonrt.ParseString(data, p)
	}
	if p < len(data) && data[p] == '"' {
		if q := bytes.IndexAny(data[p+1:], "\\\""); q >= 0 && data[p+1+q] == '"' {
			return string(data[p+1 : p+1+q]), p + q + 2, nil
		}
	}
	return odjsonrt.ParseString(data, p)
}

// parseAuthor is the shape of odjson's generated struct decoder.
func parseAuthor(data []byte, v *Author, p int, tr trusted) (int, error) {
	var err error
	p = odjsonrt.SkipSpace(data, p)
	if np, ok := odjsonrt.ParseNull(data, p); ok {
		return np, nil
	}
	if p >= len(data) || data[p] != '{' {
		return p, odjsonrt.ErrType(data, p, "proto.Author")
	}
	p++
	p = odjsonrt.SkipSpace(data, p)
	if p < len(data) && data[p] == '}' {
		return p + 1, nil
	}
	for {
		var key []byte
		p = odjsonrt.SkipSpace(data, p)
		if key, _, p, err = odjsonrt.ParseKey(data, p); err != nil {
			return p, err
		}
		p = odjsonrt.SkipSpace(data, p)
		switch string(key) {
		case "name":
			v.Name, p, err = tr.str(data, p)
		case "age":
			var n int64
			n, p, err = odjsonrt.ParseInt(data, p, 64)
			v.Age = int(n)
		case "male":
			v.Male, p, err = odjsonrt.ParseBool(data, p)
		default:
			p, err = odjsonrt.SkipValue(data, p)
		}
		if err != nil {
			return p, err
		}
		p = odjsonrt.SkipSpace(data, p)
		if p >= len(data) {
			return p, odjsonrt.ErrSyntax(data, p, "unexpected end of JSON input")
		}
		switch data[p] {
		case ',':
			p++
		case '}':
			return p + 1, nil
		default:
			return p, odjsonrt.ErrSyntax(data, p, "after object key:value pair")
		}
	}
}

func parseBook(data []byte, v *TokBook, p int, tr trusted) (int, error) {
	var err error
	p = odjsonrt.SkipSpace(data, p)
	if np, ok := odjsonrt.ParseNull(data, p); ok {
		return np, nil
	}
	if p >= len(data) || data[p] != '{' {
		return p, odjsonrt.ErrType(data, p, "proto.Book")
	}
	p++
	p = odjsonrt.SkipSpace(data, p)
	if p < len(data) && data[p] == '}' {
		return p + 1, nil
	}
	for {
		var key []byte
		p = odjsonrt.SkipSpace(data, p)
		if key, _, p, err = odjsonrt.ParseKey(data, p); err != nil {
			return p, err
		}
		p = odjsonrt.SkipSpace(data, p)
		switch string(key) {
		case "id":
			var n int64
			n, p, err = odjsonrt.ParseInt(data, p, 64)
			v.BookId = int(n)
		case "ids":
			v.BookIds, p, err = parseIntsBytes(data, p, v.BookIds)
		case "title":
			v.Title, p, err = tr.str(data, p)
		case "titles":
			p, err = parseSlice(data, p, &v.Titles, func(d []byte, q int) (string, int, error) {
				return tr.str(d, q)
			})
		case "price":
			v.Price, p, err = odjsonrt.ParseFloat(data, p, 64)
		case "prices":
			p, err = parseSlice(data, p, &v.Prices, func(d []byte, q int) (float64, int, error) {
				return odjsonrt.ParseFloat(d, q, 64)
			})
		case "hot":
			v.Hot, p, err = odjsonrt.ParseBool(data, p)
		case "hots":
			p, err = parseSlice(data, p, &v.Hots, odjsonrt.ParseBool)
		case "author":
			p, err = parseAuthor(data, &v.Author, p, tr)
		case "authors":
			p, err = parseSlice(data, p, &v.Authors, func(d []byte, q int) (Author, int, error) {
				var a Author
				n, err := parseAuthor(d, &a, q, tr)
				return a, n, err
			})
		case "weights":
			v.Weights, p, err = parseIntsBytes(data, p, v.Weights)
		default:
			p, err = odjsonrt.SkipValue(data, p)
		}
		if err != nil {
			return p, err
		}
		p = odjsonrt.SkipSpace(data, p)
		if p >= len(data) {
			return p, odjsonrt.ErrSyntax(data, p, "unexpected end of JSON input")
		}
		switch data[p] {
		case ',':
			p++
		case '}':
			return p + 1, nil
		default:
			return p, odjsonrt.ErrSyntax(data, p, "after object key:value pair")
		}
	}
}

func parseIntsBytes(data []byte, p int, dst []int) ([]int, int, error) {
	err := error(nil)
	p, err = parseSlice(data, p, &dst, func(d []byte, q int) (int, int, error) {
		n, q, err := odjsonrt.ParseInt(d, q, 64)
		return int(n), q, err
	})
	return dst, p, err
}

func parseSlice[T any](data []byte, p int, dst *[]T, elem func([]byte, int) (T, int, error)) (int, error) {
	p = odjsonrt.SkipSpace(data, p)
	if np, ok := odjsonrt.ParseNull(data, p); ok {
		*dst = nil
		return np, nil
	}
	if p >= len(data) || data[p] != '[' {
		return p, odjsonrt.ErrType(data, p, "slice")
	}
	p++
	s := (*dst)[:0]
	p = odjsonrt.SkipSpace(data, p)
	if p < len(data) && data[p] == ']' {
		p++
	} else {
		for {
			var e T
			var err error
			p = odjsonrt.SkipSpace(data, p)
			if e, p, err = elem(data, p); err != nil {
				return p, err
			}
			s = append(s, e)
			p = odjsonrt.SkipSpace(data, p)
			if p >= len(data) {
				return p, odjsonrt.ErrSyntax(data, p, "unexpected end of JSON input")
			}
			if data[p] == ',' {
				p++
				continue
			}
			if data[p] == ']' {
				p++
				break
			}
			return p, odjsonrt.ErrSyntax(data, p, "after array element")
		}
	}
	if s == nil {
		s = []T{}
	}
	*dst = s
	return p, nil
}
