package proto

import "encoding/json/jsontext"

// v2SafeSet marks the bytes that may be copied into a JSON string verbatim
// when the value is destined for jsontext.Encoder.WriteValue.
//
// It is deliberately weaker than odjsonrt's table:
//
//   - HTML characters are not escaped. encoding/json/v2 does not escape them,
//     so escaping here only makes jsontext unescape them again.
//   - Bytes >= 0x80 are copied without decoding. jsontext validates UTF-8
//     while reformatting the value and reports the error, so validating here
//     is redundant work on every string.
var v2SafeSet = func() (t [256]bool) {
	for c := range t {
		t[c] = c >= 0x20 && c != '"' && c != '\\'
	}
	return t
}()

const hexDigits = "0123456789abcdef"

// appendStringFast quotes s for a value that jsontext will reformat.
func appendStringFast(dst []byte, s string) []byte {
	dst = append(dst, '"')
	last := 0
	for i := 0; i < len(s); i++ {
		if v2SafeSet[s[i]] {
			continue
		}
		dst = append(dst, s[last:i]...)
		switch c := s[i]; c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		last = i + 1
	}
	dst = append(dst, s[last:]...)
	return append(dst, '"')
}

// FastBook builds its value with appendStringFast, leaving HTML escaping and
// UTF-8 validation to jsontext, which performs them anyway.
type FastBook Book

func appendAuthorFast(dst []byte, v *Author) []byte {
	dst = append(dst, `{"name":`...)
	dst = appendStringFast(dst, v.Name)
	dst = append(dst, `,"age":`...)
	dst = appendIntFast(dst, int64(v.Age))
	dst = append(dst, `,"male":`...)
	dst = appendBoolFast(dst, v.Male)
	return append(dst, '}')
}

// MarshalJSONTo implements jsonv2.MarshalerTo.
func (v FastBook) MarshalJSONTo(enc *jsontext.Encoder) error {
	b := enc.AvailableBuffer()
	b = append(b, `{"id":`...)
	b = appendIntFast(b, int64(v.BookId))
	b = append(b, `,"ids":`...)
	b = appendIntsFast(b, v.BookIds)
	b = append(b, `,"title":`...)
	b = appendStringFast(b, v.Title)
	b = append(b, `,"titles":`...)
	if v.Titles == nil {
		b = append(b, `null`...)
	} else {
		b = append(b, '[')
		for i := range v.Titles {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendStringFast(b, v.Titles[i])
		}
		b = append(b, ']')
	}
	b = append(b, `,"price":`...)
	var err error
	if b, err = appendFloatFast(b, v.Price); err != nil {
		return err
	}
	b = append(b, `,"prices":`...)
	if v.Prices == nil {
		b = append(b, `null`...)
	} else {
		b = append(b, '[')
		for i := range v.Prices {
			if i > 0 {
				b = append(b, ',')
			}
			if b, err = appendFloatFast(b, v.Prices[i]); err != nil {
				return err
			}
		}
		b = append(b, ']')
	}
	b = append(b, `,"hot":`...)
	b = appendBoolFast(b, v.Hot)
	b = append(b, `,"hots":`...)
	if v.Hots == nil {
		b = append(b, `null`...)
	} else {
		b = append(b, '[')
		for i := range v.Hots {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendBoolFast(b, v.Hots[i])
		}
		b = append(b, ']')
	}
	b = append(b, `,"author":`...)
	b = appendAuthorFast(b, &v.Author)
	b = append(b, `,"authors":`...)
	if v.Authors == nil {
		b = append(b, `null`...)
	} else {
		b = append(b, '[')
		for i := range v.Authors {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendAuthorFast(b, &v.Authors[i])
		}
		b = append(b, ']')
	}
	b = append(b, `,"weights"`...)
	b = append(b, ':')
	b = appendIntsFast(b, v.Weights)
	b = append(b, '}')
	return enc.WriteValue(b)
}

// UnmarshalJSONFrom keeps odjson's byte oriented parser but tells it that
// jsontext has already validated the UTF-8 of every string in the value.
func (v *FastBook) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	_, err = parseBook(val, (*TokBook)(v), 0, true)
	return err
}
