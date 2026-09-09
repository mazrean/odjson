// Package plain declares embed's types a second time, with no generated codec
// attached, so encoding/json's own promotion rules can be observed. See
// [github.com/mazrean/odjson/internal/testfixture/plainref].
package plain

// Label is an embedded non-struct named type, which is promoted as a single
// field named after the type.
type Label string

// Deep is embedded two levels down.
type Deep struct {
	DeepOnly string `json:"deep_only"`
	Shared   string `json:"shared"`
}

// Mid embeds Deep and shadows one of its members at a shallower depth.
type Mid struct {
	Deep
	Shared  string `json:"shared"`
	MidOnly int    `json:"mid_only"`
}

// LeftConflict and RightConflict both contribute "Clash" at the same depth, so
// encoding/json drops the name entirely. The field is deliberately untagged:
// go vet's structtag check rejects the same json tag appearing twice, but the
// promotion rule under test applies to untagged names just the same.
type LeftConflict struct {
	Clash string
	Left  string `json:"left"`
}

// RightConflict is the other half of the dropped-name case.
type RightConflict struct {
	Clash string
	Right string `json:"right"`
}

// UntaggedSide contributes an untagged Winner field.
type UntaggedSide struct {
	Winner string
}

// TaggedSide contributes a tagged winner field, which beats the untagged one
// at the same depth.
type TaggedSide struct {
	Winner string `json:"Winner"`
}

// PtrPart is embedded by pointer.
type PtrPart struct {
	PtrOnly int `json:"ptr_only"`
}

// Named is embedded under an explicit JSON name, so it stays a single object
// member instead of being flattened.
type Named struct {
	Value int `json:"value"`
}

// Promoted is the type under test.
type Promoted struct {
	Mid
	LeftConflict
	RightConflict
	UntaggedSide
	TaggedSide
	*PtrPart
	Label
	Named `json:"named"`

	Own string `json:"own"`
}

// ShallowWins checks that a field declared on the outer struct beats a
// promoted one of the same name.
type ShallowWins struct {
	Deep
	Shared int `json:"shared"`
}
