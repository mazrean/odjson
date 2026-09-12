//go:build odjson_safe

package odjsonrt

import "encoding/json/jsontext"

// With the odjson_safe build tag the direct path (see direct.go) is compiled
// out: the generated methods only ever use jsontext's public API.

// BeginDirectEncodeMode always declines under the odjson_safe build tag.
func BeginDirectEncodeMode(enc *jsontext.Encoder) ([]byte, StringMode, bool) {
	return nil, 0, false
}

// BeginDirectEncode always declines under the odjson_safe build tag.
func BeginDirectEncode(enc *jsontext.Encoder) ([]byte, bool) { return nil, false }

// EndDirectEncode is never reached under the odjson_safe build tag.
func EndDirectEncode(enc *jsontext.Encoder, buf []byte) {}

// BeginDirectDecodeAt always declines under the odjson_safe build tag.
func BeginDirectDecodeAt(dec *jsontext.Decoder) ([]byte, int, bool) { return nil, 0, false }

// BeginDirectDecode always declines under the odjson_safe build tag.
func BeginDirectDecode(dec *jsontext.Decoder) ([]byte, bool) { return nil, false }

// EndDirectDecodeAt is never reached under the odjson_safe build tag.
func EndDirectDecodeAt(dec *jsontext.Decoder, start, end int) {}

// EndDirectDecode is never reached under the odjson_safe build tag.
func EndDirectDecode(dec *jsontext.Decoder, end int) {}

// DirectEnabled reports whether the direct path is active in this binary.
func DirectEnabled() bool { return false }
