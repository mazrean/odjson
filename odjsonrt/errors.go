package odjsonrt

import "strconv"

// SyntaxError describes a JSON syntax error found at a byte offset in the
// document being decoded.
type SyntaxError struct {
	// Msg is the human readable description of the error.
	Msg string
	// Offset is the byte offset in the document where the error was found.
	Offset int64
}

// Error implements the error interface.
func (e *SyntaxError) Error() string { return e.Msg }

// TypeError describes a JSON value that could not be stored into a Go value
// of a particular type. It mirrors encoding/json's UnmarshalTypeError.
type TypeError struct {
	// Value describes the JSON value, e.g. "string", "number", "bool",
	// "object", "array" or "null".
	Value string
	// Type is the name of the Go type the value could not be stored into.
	Type string
	// Offset is the byte offset in the document where the value starts.
	Offset int64
	// Field is the (optional) name of the struct field being decoded.
	Field string
}

// Error implements the error interface.
func (e *TypeError) Error() string {
	if e.Field != "" {
		return "json: cannot unmarshal " + e.Value + " into Go struct field " + e.Field + " of type " + e.Type
	}
	return "json: cannot unmarshal " + e.Value + " into Go value of type " + e.Type
}

// UnknownFieldError is returned by [ErrUnknownField] when a document contains
// an object member that the target struct does not define and the generated
// code was compiled with DisallowUnknownFields semantics.
type UnknownFieldError struct {
	// Field is the name of the unknown object member.
	Field string
}

// Error implements the error interface.
func (e *UnknownFieldError) Error() string {
	return "json: unknown field " + strconv.Quote(e.Field)
}

// ErrSyntax builds a [SyntaxError] for the value at pos with the given
// message. The document is accepted for symmetry with [ErrType] and to allow
// richer diagnostics in the future; it is not otherwise inspected.
func ErrSyntax(data []byte, pos int, msg string) error {
	_ = data
	return &SyntaxError{Msg: msg, Offset: int64(pos)}
}

// ErrType builds a [TypeError] for the JSON value that starts at pos,
// inferring the JSON value kind from its first byte. If that byte cannot
// start a JSON value a [SyntaxError] is returned instead.
func ErrType(data []byte, pos int, goType string) error {
	kind := valueKind(data, pos)
	if kind == "" {
		return errBeginValue(data, pos)
	}
	return &TypeError{Value: kind, Type: goType, Offset: int64(pos)}
}

// ErrTypeField is [ErrType] with the name of the struct field being decoded
// attached to the resulting [TypeError].
func ErrTypeField(data []byte, pos int, goType, field string) error {
	err := ErrType(data, pos, goType)
	if te, ok := err.(*TypeError); ok {
		te.Field = field
	}
	return err
}

// ErrUnknownField builds an [UnknownFieldError] for the named object member.
func ErrUnknownField(name string) error {
	return &UnknownFieldError{Field: name}
}

// valueKind reports the JSON kind of the value starting at pos, or "" if the
// byte at pos cannot start a JSON value.
func valueKind(data []byte, pos int) string {
	if pos < 0 || pos >= len(data) {
		return ""
	}
	switch c := data[pos]; c {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	default:
		if c == '-' || (c >= '0' && c <= '9') {
			return "number"
		}
		return ""
	}
}

// errUnexpectedEnd reports a truncated document.
func errUnexpectedEnd(pos int) error {
	return &SyntaxError{Msg: "unexpected end of JSON input", Offset: int64(pos)}
}

// errBeginValue reports a byte that cannot start a JSON value.
func errBeginValue(data []byte, pos int) error {
	if pos >= len(data) {
		return errUnexpectedEnd(pos)
	}
	return &SyntaxError{
		Msg:    "invalid character " + quoteChar(data[pos]) + " looking for beginning of value",
		Offset: int64(pos),
	}
}

// errChar reports an unexpected byte with the given contextual message.
func errChar(data []byte, pos int, what string) error {
	if pos >= len(data) {
		return errUnexpectedEnd(pos)
	}
	return &SyntaxError{
		Msg:    "invalid character " + quoteChar(data[pos]) + " " + what,
		Offset: int64(pos),
	}
}

// quoteChar formats c as a single-quoted character literal, the way
// encoding/json does in its syntax error messages.
func quoteChar(c byte) string {
	switch c {
	case '\'':
		return `'\''`
	case '"':
		return `'"'`
	}
	s := strconv.Quote(string(rune(c)))
	return "'" + s[1:len(s)-1] + "'"
}
