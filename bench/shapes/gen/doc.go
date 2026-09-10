// Package gen holds the types behind bench/shapes with odjson's generated
// codecs attached. Its types.go must stay byte for byte the same as
// plain/types.go but for the package clause; bench/shapes tests that.
package gen

//go:generate go run github.com/mazrean/odjson
