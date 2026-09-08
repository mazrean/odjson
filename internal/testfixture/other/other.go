// Package other holds struct types that are referenced from another package
// but never generated for themselves, so odjson has to emit package level
// helper functions instead of methods.
package other

// Thing is referenced from package crosspkg.
type Thing struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// Wrapper nests another foreign struct, so the helper functions have to call
// each other.
type Wrapper struct {
	Thing Thing   `json:"thing"`
	Ptr   *Thing  `json:"ptr"`
	List  []Thing `json:"list"`
}

// hidden is unexported and therefore cannot be referenced from outside; it
// exists to make sure the analyser does not try to generate for it.
type hidden struct {
	n int //nolint:unused
}
