// Package unexported covers the default type selection, which takes every
// struct declared in the package, unexported ones included. A generated method
// on an unexported type is still what encoding/json calls once a value of that
// type is reached, so leaving those types out only pushed them back onto
// reflection.
package unexported

//go:generate go run github.com/mazrean/odjson

// secret is unexported. It is selected as a root of its own and is also
// reachable from Holder, which must not emit its codec twice.
type secret struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// lone is unexported and reached from nothing, so only the default selection
// puts it in.
type lone struct {
	Flag bool `json:"flag"`
}

// Holder reaches an unexported type through exported fields, which is how
// encoding/json outside this package gets to its generated methods.
type Holder struct {
	Secret secret   `json:"secret"`
	Ptr    *secret  `json:"ptr"`
	List   []secret `json:"list"`
}
