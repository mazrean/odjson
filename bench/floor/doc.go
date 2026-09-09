// Package floor measures what a host JSON library charges for routing through
// json.Marshaler and json.Unmarshaler at all, independently of how fast the
// generated codec behind those interfaces is.
//
// The marshaler in the benchmarks returns an already encoded document and the
// unmarshaler discards its input, so the numbers are a lower bound for *any*
// implementation of those interfaces. Where that bound already exceeds what
// the library's own reflection path costs, no code generator can win through
// the interface, however fast its own encoding is.
package floor
