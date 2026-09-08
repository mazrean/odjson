package proto

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"

	"github.com/mazrean/odjson/odjsonrt"
)

// MarshalJSONTo drives the encoder token by token, so jsontext formats each
// value once instead of reformatting a value odjson already formatted.
func (v TokBook) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := writeName(enc, "id"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Int(int64(v.BookId))); err != nil {
		return err
	}
	if err := writeName(enc, "ids"); err != nil {
		return err
	}
	if err := writeInts(enc, v.BookIds); err != nil {
		return err
	}
	if err := writeName(enc, "title"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(v.Title)); err != nil {
		return err
	}
	if err := writeName(enc, "titles"); err != nil {
		return err
	}
	if v.Titles == nil {
		if err := enc.WriteToken(jsontext.Null); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for _, s := range v.Titles {
			if err := enc.WriteToken(jsontext.String(s)); err != nil {
				return err
			}
		}
		if err := enc.WriteToken(jsontext.EndArray); err != nil {
			return err
		}
	}
	if err := writeName(enc, "price"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Float(v.Price)); err != nil {
		return err
	}
	if err := writeName(enc, "prices"); err != nil {
		return err
	}
	if v.Prices == nil {
		if err := enc.WriteToken(jsontext.Null); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for _, f := range v.Prices {
			if err := enc.WriteToken(jsontext.Float(f)); err != nil {
				return err
			}
		}
		if err := enc.WriteToken(jsontext.EndArray); err != nil {
			return err
		}
	}
	if err := writeName(enc, "hot"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Bool(v.Hot)); err != nil {
		return err
	}
	if err := writeName(enc, "hots"); err != nil {
		return err
	}
	if v.Hots == nil {
		if err := enc.WriteToken(jsontext.Null); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for _, b := range v.Hots {
			if err := enc.WriteToken(jsontext.Bool(b)); err != nil {
				return err
			}
		}
		if err := enc.WriteToken(jsontext.EndArray); err != nil {
			return err
		}
	}
	if err := writeName(enc, "author"); err != nil {
		return err
	}
	if err := writeAuthor(enc, &v.Author); err != nil {
		return err
	}
	if err := writeName(enc, "authors"); err != nil {
		return err
	}
	if v.Authors == nil {
		if err := enc.WriteToken(jsontext.Null); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for i := range v.Authors {
			if err := writeAuthor(enc, &v.Authors[i]); err != nil {
				return err
			}
		}
		if err := enc.WriteToken(jsontext.EndArray); err != nil {
			return err
		}
	}
	if err := writeName(enc, "weights"); err != nil {
		return err
	}
	if err := writeInts(enc, v.Weights); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// writeName writes an object member name.
func writeName(enc *jsontext.Encoder, name string) error {
	return enc.WriteToken(jsontext.String(name))
}

func writeInts(enc *jsontext.Encoder, s []int) error {
	if s == nil {
		return enc.WriteToken(jsontext.Null)
	}
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, n := range s {
		if err := enc.WriteToken(jsontext.Int(int64(n))); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndArray)
}

func writeAuthor(enc *jsontext.Encoder, a *Author) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := writeName(enc, "name"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(a.Name)); err != nil {
		return err
	}
	if err := writeName(enc, "age"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Int(int64(a.Age))); err != nil {
		return err
	}
	if err := writeName(enc, "male"); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Bool(a.Male)); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// UnmarshalJSONFrom drives the decoder token by token: the document is parsed
// exactly once, by jsontext, and the scalars are converted straight into
// fields.
func (v *TokBook) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == 'n' {
		_, err := dec.ReadToken()
		return err
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	for dec.PeekKind() != '}' {
		name, err := dec.ReadValue()
		if err != nil {
			return err
		}
		switch string(name) {
		case `"id"`:
			n, err := readInt(dec)
			if err != nil {
				return err
			}
			v.BookId = int(n)
		case `"ids"`:
			if err := readInts(dec, &v.BookIds); err != nil {
				return err
			}
		case `"title"`:
			s, err := readString(dec)
			if err != nil {
				return err
			}
			v.Title = s
		case `"titles"`:
			if err := readArray(dec, &v.Titles, readString); err != nil {
				return err
			}
		case `"price"`:
			f, err := readFloat(dec)
			if err != nil {
				return err
			}
			v.Price = f
		case `"prices"`:
			if err := readArray(dec, &v.Prices, readFloat); err != nil {
				return err
			}
		case `"hot"`:
			b, err := readBool(dec)
			if err != nil {
				return err
			}
			v.Hot = b
		case `"hots"`:
			if err := readArray(dec, &v.Hots, readBool); err != nil {
				return err
			}
		case `"author"`:
			if err := readAuthor(dec, &v.Author); err != nil {
				return err
			}
		case `"authors"`:
			if err := readArray(dec, &v.Authors, func(d *jsontext.Decoder) (Author, error) {
				var a Author
				return a, readAuthor(d, &a)
			}); err != nil {
				return err
			}
		case `"weights"`:
			if err := readInts(dec, &v.Weights); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
	_, err := dec.ReadToken()
	return err
}

func readAuthor(dec *jsontext.Decoder, a *Author) error {
	if dec.PeekKind() == 'n' {
		_, err := dec.ReadToken()
		return err
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	for dec.PeekKind() != '}' {
		name, err := dec.ReadValue()
		if err != nil {
			return err
		}
		switch string(name) {
		case `"name"`:
			if a.Name, err = readString(dec); err != nil {
				return err
			}
		case `"age"`:
			n, err := readInt(dec)
			if err != nil {
				return err
			}
			a.Age = int(n)
		case `"male"`:
			if a.Male, err = readBool(dec); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
	_, err := dec.ReadToken()
	return err
}

func readInt(dec *jsontext.Decoder) (int64, error) {
	val, err := dec.ReadValue()
	if err != nil {
		return 0, err
	}
	n, _, err := odjsonrt.ParseInt(val, 0, 64)
	return n, err
}

func readFloat(dec *jsontext.Decoder) (float64, error) {
	val, err := dec.ReadValue()
	if err != nil {
		return 0, err
	}
	f, _, err := odjsonrt.ParseFloat(val, 0, 64)
	return f, err
}

func readBool(dec *jsontext.Decoder) (bool, error) {
	val, err := dec.ReadValue()
	if err != nil {
		return false, err
	}
	b, _, err := odjsonrt.ParseBool(val, 0)
	return b, err
}

func readString(dec *jsontext.Decoder) (string, error) {
	val, err := dec.ReadValue()
	if err != nil {
		return "", err
	}
	// jsontext has already consumed and validated this string, so an
	// escape-free literal needs nothing but a copy. odjsonrt.ParseString
	// would rescan it, including a full UTF-8 pass.
	if b := val[1 : len(val)-1]; bytes.IndexByte(b, '\\') < 0 {
		return string(b), nil
	}
	s, _, err := odjsonrt.ParseString(val, 0)
	return s, err
}

func readInts(dec *jsontext.Decoder, dst *[]int) error {
	return readArray(dec, dst, func(d *jsontext.Decoder) (int, error) {
		n, err := readInt(d)
		return int(n), err
	})
}

func readArray[T any](dec *jsontext.Decoder, dst *[]T, elem func(*jsontext.Decoder) (T, error)) error {
	switch k := dec.PeekKind(); k {
	case 'n':
		_, err := dec.ReadToken()
		*dst = nil
		return err
	case '[':
	default:
		return fmt.Errorf("proto: expected an array, found %v", k)
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	s := (*dst)[:0]
	for dec.PeekKind() != ']' {
		e, err := elem(dec)
		if err != nil {
			return err
		}
		s = append(s, e)
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	if s == nil {
		s = []T{}
	}
	*dst = s
	return nil
}
