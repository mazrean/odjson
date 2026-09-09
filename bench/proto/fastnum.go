package proto

import (
	"strconv"

	"github.com/mazrean/odjson/odjsonrt"
)

func appendIntFast(dst []byte, v int64) []byte { return strconv.AppendInt(dst, v, 10) }

func appendBoolFast(dst []byte, v bool) []byte {
	if v {
		return append(dst, "true"...)
	}
	return append(dst, "false"...)
}

func appendFloatFast(dst []byte, v float64) ([]byte, error) {
	return odjsonrt.AppendFloat(dst, v, 64)
}

func appendIntsFast(dst []byte, s []int) []byte {
	if s == nil {
		return append(dst, `null`...)
	}
	dst = append(dst, '[')
	for i := range s {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendIntFast(dst, int64(s[i]))
	}
	return append(dst, ']')
}
