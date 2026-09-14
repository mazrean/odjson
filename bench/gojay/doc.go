// Package gojay holds the same vendored payload types as package plain, with
// francoispqt/gojay's MarshalerJSONObject / UnmarshalerJSONObject
// implementations attached, the way package gen holds them with odjson's
// generated codecs.
//
// The implementations in codec.go are written by hand. gojay ships a
// generator, but it stops at the first interface{} field ("Unknown type
// interface{}"), and TwitterStruct has fourteen of them; hand-written code
// against gojay's encoder and decoder API is what a gojay user has to write
// for these types, so that is what the row measures. TestParity in
// bench_test.go holds that code to encoding/json's reading of the fixtures.
//
// The types are the same files as plain's but for the package clause.
package gojay
