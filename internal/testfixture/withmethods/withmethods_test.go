package withmethods

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"
)

var (
	_ json.Marshaler   = Person{}
	_ json.Unmarshaler = (*Person)(nil)
)

func sample() Person {
	email := "a@example.com"
	return Person{
		Name:    "Hiro",
		Age:     14,
		Email:   &email,
		Tags:    []string{"genius", "quiet"},
		Address: Address{Street: "1-2-3", City: "Tokyo"},
	}
}

// TestLibrariesAgree checks that both stdlib entry points route through the
// generated codec and produce the same bytes as MarshalJSON does on its own.
func TestLibrariesAgree(t *testing.T) {
	v := sample()
	direct, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	v1, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, v1) {
		t.Errorf("encoding/json: got %s, want %s", v1, direct)
	}

	v2, err := jsonv2.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, v2) {
		t.Errorf("encoding/json/v2: got %s, want %s", v2, direct)
	}
}

func TestDecodeThroughLibraries(t *testing.T) {
	want := sample()
	data, err := want.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	var a Person
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("encoding/json: got %#v, want %#v", a, want)
	}

	var b Person
	if err := jsonv2.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("encoding/json/v2: got %#v, want %#v", b, want)
	}
}

// TestUnmarshalNullIsNoOp mirrors encoding/json, which calls UnmarshalJSON with
// the literal null and expects the value to be left alone.
func TestUnmarshalNullIsNoOp(t *testing.T) {
	v := sample()
	if err := v.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, sample()) {
		t.Errorf("null changed the value: %#v", v)
	}
}

// TestNestedValueUsesGeneratedCodec makes sure a struct embedded as a field is
// encoded by the generated helper rather than by reflection.
func TestNestedValueUsesGeneratedCodec(t *testing.T) {
	a := Address{Street: "s"}
	got, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"street":"s"}` {
		t.Errorf("got %s", got)
	}
}
