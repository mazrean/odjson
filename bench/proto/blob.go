package proto

import (
	"encoding/json/jsontext"

	"github.com/mazrean/odjson/odjsonrt"
)

// appendAuthor is the shape of odjson's generated struct encoder.
func appendAuthor(dst []byte, v *Author) []byte {
	dst = append(dst, `{"name":`...)
	dst = odjsonrt.AppendString(dst, v.Name, true)
	dst = append(dst, `,"age":`...)
	dst = odjsonrt.AppendInt(dst, int64(v.Age))
	dst = append(dst, `,"male":`...)
	dst = odjsonrt.AppendBool(dst, v.Male)
	return append(dst, '}')
}

func appendBook(dst []byte, v *BlobBook) ([]byte, error) {
	var err error
	dst = append(dst, `{"id":`...)
	dst = odjsonrt.AppendInt(dst, int64(v.BookId))
	dst = append(dst, `,"ids":`...)
	dst = appendInts(dst, v.BookIds)
	dst = append(dst, `,"title":`...)
	dst = odjsonrt.AppendString(dst, v.Title, true)
	dst = append(dst, `,"titles":`...)
	if v.Titles == nil {
		dst = append(dst, `null`...)
	} else {
		dst = append(dst, '[')
		for i := range v.Titles {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = odjsonrt.AppendString(dst, v.Titles[i], true)
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"price":`...)
	if dst, err = odjsonrt.AppendFloat(dst, v.Price, 64); err != nil {
		return dst, err
	}
	dst = append(dst, `,"prices":`...)
	if v.Prices == nil {
		dst = append(dst, `null`...)
	} else {
		dst = append(dst, '[')
		for i := range v.Prices {
			if i > 0 {
				dst = append(dst, ',')
			}
			if dst, err = odjsonrt.AppendFloat(dst, v.Prices[i], 64); err != nil {
				return dst, err
			}
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"hot":`...)
	dst = odjsonrt.AppendBool(dst, v.Hot)
	dst = append(dst, `,"hots":`...)
	if v.Hots == nil {
		dst = append(dst, `null`...)
	} else {
		dst = append(dst, '[')
		for i := range v.Hots {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = odjsonrt.AppendBool(dst, v.Hots[i])
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"author":`...)
	dst = appendAuthor(dst, &v.Author)
	dst = append(dst, `,"authors":`...)
	if v.Authors == nil {
		dst = append(dst, `null`...)
	} else {
		dst = append(dst, '[')
		for i := range v.Authors {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendAuthor(dst, &v.Authors[i])
		}
		dst = append(dst, ']')
	}
	dst = append(dst, `,"weights":`...)
	dst = appendInts(dst, v.Weights)
	return append(dst, '}'), nil
}

func appendInts(dst []byte, s []int) []byte {
	if s == nil {
		return append(dst, `null`...)
	}
	dst = append(dst, '[')
	for i := range s {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = odjsonrt.AppendInt(dst, int64(s[i]))
	}
	return append(dst, ']')
}

// MarshalJSONTo builds the whole value first, then writes it in one call.
func (v BlobBook) MarshalJSONTo(enc *jsontext.Encoder) error {
	buf, err := appendBook(odjsonrt.AcquireBuffer(), &v)
	if err != nil {
		odjsonrt.ReleaseBuffer(buf)
		return err
	}
	err = enc.WriteValue(buf)
	odjsonrt.ReleaseBuffer(buf)
	return err
}

// UnmarshalJSONFrom reads the whole value, then parses it with odjson's byte
// oriented parser.
func (v *BlobBook) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	_, err = parseBook(val, (*TokBook)(v), 0)
	return err
}
