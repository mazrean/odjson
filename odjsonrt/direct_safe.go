//go:build odjson_safe

package odjsonrt

import "encoding/json/jsontext"

// With the odjson_safe build tag the direct path (see direct.go) is compiled
// out: the generated methods only ever use jsontext's public API.

// BeginDirectEncode always declines under the odjson_safe build tag.
func BeginDirectEncode(enc *jsontext.Encoder) ([]byte, bool) { return nil, false }

// EndDirectEncode is never reached under the odjson_safe build tag.
func EndDirectEncode(enc *jsontext.Encoder, buf []byte) {}

// BeginDirectDecode always declines under the odjson_safe build tag.
func BeginDirectDecode(dec *jsontext.Decoder) ([]byte, bool) { return nil, false }

// EndDirectDecode is never reached under the odjson_safe build tag.
func EndDirectDecode(dec *jsontext.Decoder, end int) {}

// DirectEnabled reports whether the direct path is active in this binary.
func DirectEnabled() bool { return false }
