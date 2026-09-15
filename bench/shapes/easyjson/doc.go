// Package easyjson holds the types behind bench/shapes with mailru/easyjson's
// generated codecs attached, the way package gen holds them with odjson's:
// easyjson is a code generator too, so its rows are the ones that compare
// like with like against odjson's on every shape. Its types.go must stay
// byte for byte the same as plain/types.go but for the package clause;
// bench/shapes tests that.
//
// -no_std_marshalers leaves MarshalJSON / UnmarshalJSON off the types, so
// easyjson is reached through its own entry points and nothing else changes.
package easyjson

//go:generate go run github.com/mailru/easyjson/easyjson -all -no_std_marshalers types.go
