// Package easyjson holds the same vendored payload types as package plain,
// with mailru/easyjson's generated codecs attached, the way package gen holds
// them with odjson's. easyjson is a code generator too, so its row is the one
// that compares like with like against odjson's.
//
// The types are the same files as plain's but for the package clause and
// two `// easyjson:skip` comments in small.go, which sonic carries to keep
// easyjson out of its own benchmark and which would keep it out of this one.
// -no_std_marshalers leaves MarshalJSON / UnmarshalJSON off the types, so
// easyjson is reached through its own entry points and nothing else changes.
package easyjson

//go:generate go run github.com/mailru/easyjson/easyjson -all -no_std_marshalers twitter.go small.go
