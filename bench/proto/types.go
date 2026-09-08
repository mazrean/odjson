// Package proto is a throwaway experiment, not part of odjson's build. It
// compares three ways of implementing encoding/json/v2's MarshalerTo and
// UnmarshalerFrom for the same struct, to find out whether a token-driven
// implementation can beat json/v2's own reflection path — which the current,
// value-driven generated code does not.
package proto

// Author is copied from sonic's testdata.
type Author struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
	Male bool   `json:"male"`
}

// Book is copied from sonic's testdata. It has no methods, so json/v2 encodes
// and decodes it by reflection: the baseline.
type Book struct {
	BookId  int       `json:"id"`
	BookIds []int     `json:"ids"`
	Title   string    `json:"title"`
	Titles  []string  `json:"titles"`
	Price   float64   `json:"price"`
	Prices  []float64 `json:"prices"`
	Hot     bool      `json:"hot"`
	Hots    []bool    `json:"hots"`
	Author  Author    `json:"author"`
	Authors []Author  `json:"authors"`
	Weights []int     `json:"weights"`
}

// BlobBook mirrors what odjson generates today: build the whole value in a
// byte slice, then hand it to the encoder in one piece.
type BlobBook Book

// TokBook is the experiment: drive the encoder and decoder token by token, so
// the value is formatted and parsed exactly once.
type TokBook Book
