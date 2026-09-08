// Package odjsonrt provides the low-level runtime helpers that odjson's
// generated Marshal/Unmarshal code calls into.
//
// Encoding helpers append directly to a caller-owned byte slice; decoding
// helpers operate on the complete document and an index, returning the index
// just past the value they consumed. Every helper is written to match the
// observable behaviour of encoding/json (v1) so that generated code is a
// drop-in replacement for the standard library.
//
// Unless documented otherwise, decoding helpers expect that leading
// whitespace has already been consumed by the caller via [SkipSpace], and
// that the index passed in points at the first byte of a JSON value.
//
// Scalar helpers stop at the end of the literal they recognise and do not
// look at what follows: ParseInt on "01" reads the 0 and reports index 1.
// Checking that a valid delimiter follows a value is the caller's job, the
// same way [SkipValue] does it for nested values.
//
// The reference for "matches encoding/json" is the standard library of the
// Go toolchain this module is built with. Since Go 1.27 that package is
// implemented on top of encoding/json/v2 in v1 compatibility mode, which
// differs in a few details from the older hand written v1 encoder (invalid
// UTF-8 is replaced with a literal U+FFFD rather than a \ufffd escape, and
// U+2028/U+2029 are escaped when compacting even with HTML escaping off).
// The tests pin every one of these behaviours against the standard library.
package odjsonrt

// MaxDepth is the maximum nesting depth accepted by the scanner. It matches
// the limit enforced by encoding/json.
const MaxDepth = 10000
