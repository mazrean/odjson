package testfixture

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/mazrean/odjson/internal/testfixture/plain"
	"github.com/mazrean/odjson/internal/testfixture/plainref"
)

// The generated methods are what json.Marshal and json.Unmarshal call for
// these types, so package plain's identical declarations are what reflection
// is left to work on. TestSameLayout keeps the two in step.
var (
	scalarsRef    = plainref.Of[Scalars, plain.Scalars]
	compositesRef = plainref.Of[Composites, plain.Composites]
	recursiveRef  = plainref.Of[Recursive, plain.Recursive]
)

func TestSameLayout(t *testing.T) {
	cases := []struct {
		name string
		a, b reflect.Type
	}{
		{"Scalars", reflect.TypeFor[Scalars](), reflect.TypeFor[plain.Scalars]()},
		{"Composites", reflect.TypeFor[Composites](), reflect.TypeFor[plain.Composites]()},
		{"Recursive", reflect.TypeFor[Recursive](), reflect.TypeFor[plain.Recursive]()},
	}
	for _, tc := range cases {
		if err := plainref.SameLayout(tc.a, tc.b); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// marshalParity asserts the generated MarshalJSON is byte-for-byte identical
// to encoding/json for v. ref reads v as the twin encoding/json still reflects
// over.
//
// The method is called directly rather than through json.Marshal: with the
// json/v2 experiment on, encoding/json prefers MarshalJSONTo when a type has
// both, and that method deliberately follows json/v2's semantics. MarshalJSON
// is what sonic, go-json and a nojsonv2 build reach, and encoding/json's rules
// are what it has to keep.
func marshalParity[T, R any](t *testing.T, name string, v T, ref func(*T) *R) {
	t.Helper()
	want, wantErr := json.Marshal(ref(&v))
	got, gotErr := any(v).(json.Marshaler).MarshalJSON()
	if (wantErr != nil) != (gotErr != nil) {
		t.Errorf("%s: error mismatch: encoding/json=%v odjson=%v", name, wantErr, gotErr)
		return
	}
	if wantErr != nil {
		return
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
	}
}

// unmarshalParity asserts the generated UnmarshalJSON agrees with
// encoding/json both on the decoded value and on whether the input was
// rejected. It calls the method directly, for the reason marshalParity gives.
func unmarshalParity[T, R any](t *testing.T, name, in string, ref func(*T) *R) {
	t.Helper()
	var a, b T
	errA := json.Unmarshal([]byte(in), ref(&a))
	errB := any(&b).(json.Unmarshaler).UnmarshalJSON([]byte(in))
	if (errA != nil) != (errB != nil) {
		t.Errorf("%s: error mismatch for %s: encoding/json=%v odjson=%v", name, in, errA, errB)
		return
	}
	if errA != nil {
		return
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("%s: value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", name, in, a, b)
	}
}

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

func scalarCases() map[string]Scalars {
	n := 7
	s := "set"
	return map[string]Scalars{
		"zero": {},
		"full": {
			Base:            Base{BaseID: 1, BaseName: "base"},
			PtrBase:         &PtrBase{PtrField: "p"},
			Bool:            true,
			Int:             -1,
			Int8:            math.MinInt8,
			Int16:           math.MinInt16,
			Int32:           math.MinInt32,
			Int64:           math.MinInt64,
			Uint:            1,
			Uint8:           math.MaxUint8,
			Uint16:          math.MaxUint16,
			Uint32:          math.MaxUint32,
			Uint64:          math.MaxUint64,
			Float32:         1.5,
			Float64:         -1e21,
			String:          "a\"b\\c\n<&>\u2028\u007f日本語",
			Named:           "red",
			NamedLevel:      3,
			QuotedInt:       -42,
			QuotedBool:      true,
			QuotedString:    `he said "hi"`,
			QuotedFloat:     0.5,
			QuotedPtr:       &n,
			Skipped:         "invisible",
			Renamed:         "renamed",
			Untagged:        "untagged",
			OmitEmptyString: "x",
			OmitEmptyInt:    2,
			OmitEmptyPtr:    &s,
			OmitEmptySlice:  []int{1},
			OmitZeroTime:    time.Unix(1700000000, 0).UTC(),
			OmitZeroInner:   Inner{ID: 9, Note: "n"},
			OmitZeroString:  "z",
		},
		"floats": {
			Float32:     math.MaxFloat32,
			Float64:     1e-7,
			QuotedFloat: math.SmallestNonzeroFloat64,
		},
		"empty-strings": {String: "", Named: "", OmitEmptyPtr: new("")},
	}
}

func TestMarshalScalars(t *testing.T) {
	for name, v := range scalarCases() {
		marshalParity(t, name, v, scalarsRef)
	}
}

func TestMarshalComposites(t *testing.T) {
	inner := &Inner{ID: 2, Note: "note"}
	tm := time.Unix(1700000000, 123456789).UTC()
	cases := map[string]Composites{
		"zero": {},
		"full": {
			Bytes:     []byte{0, 1, 2, 253, 254, 255},
			NamedFlag: Flags("hello"),
			ByteArray: [4]byte{1, 2, 3, 4},
			IntArray:  [3]int{5, 6, 7},
			Ints:      []int{1, 2, 3},
			Strings:   Tags{"a", "b"},
			Inners:    []Inner{{ID: 1}, {ID: 2, Note: "x"}},
			InnerPtrs: []*Inner{nil, inner},
			Nested:    [][]int{{1}, nil, {}},
			StringMap: map[string]string{"b": "2", "a": "1", "<": ">"},
			InnerMap:  map[string]Inner{"k": {ID: 3}},
			NamedMap:  Meta{"z": 26, "a": 1},
			ColorMap:  map[Color]int{"red": 1, "blue": 2},
			Inner:     Inner{ID: 4, Note: "in"},
			InnerPtr:  inner,
			DeepPtr:   new(inner),
			Any:       map[string]any{"n": 1.0, "s": "t", "b": true, "z": nil},
			Anys:      []any{1.0, "two", nil, false, []any{1.0}},
			Raw:       json.RawMessage(`{"raw":[1,2,3]}`),
			Number:    json.Number("12345678901234567890"),
			Time:      tm,
			TimePtr:   &tm,
			Dur:       time.Second,
		},
		"empty-collections": {
			Bytes:     []byte{},
			Ints:      []int{},
			Strings:   Tags{},
			StringMap: map[string]string{},
			Anys:      []any{},
			Raw:       json.RawMessage(`null`),
		},
	}
	for name, v := range cases {
		marshalParity(t, name, v, compositesRef)
	}
}

func TestMarshalRecursive(t *testing.T) {
	v := Recursive{Name: "root", Children: []*Recursive{
		{Name: "a"},
		{Name: "b", Children: []*Recursive{{Name: "c"}}},
	}}
	marshalParity(t, "tree", v, recursiveRef)
	marshalParity(t, "leaf", Recursive{Name: "leaf"}, recursiveRef)
}

func TestUnmarshalScalars(t *testing.T) {
	inputs := []string{
		`{}`,
		`null`,
		`  {  }  `,
		`{"bool":true,"int":-5,"int8":-128,"uint64":18446744073709551615,"float64":1.5,"string":"x\u00e9\ud83d\ude00"}`,
		`{"BOOL":true,"InT":3}`,
		`{"qint":"-42","qbool":"true","qstring":"\"quoted\"","qfloat":"0.5","qptr":"7"}`,
		`{"qint":"-42","qptr":null}`,
		`{"base_id":1,"base_name":"b","ptr_field":"p"}`,
		`{"unknown":{"a":[1,2,{"b":null}]},"int":1}`,
		`{"renamed":"r","Untagged":"u"}`,
		`{"oz_time":"2023-11-14T22:13:20Z","oz_inner":{"id":1,"note":"n"}}`,
		`{"int":1,}`,
		`{"int":}`,
		`{"int":"nan"}`,
		`{"int":1.5}`,
		`{"uint":-1}`,
		`{"int8":300}`,
		`[]`,
		`{`,
		`{"a":1} trailing`,
		`{"float64":1e400}`,
		// Member names spelled in ways the raw byte match cannot see, and
		// shapes around the colon.
		`{"i\u006et":7,"b\u006fol":true}`,
		`{"int" : 7 , "bool"\t:\nfalse}`,
		`{"int"x:7}`,
		`{"int"}`,
		`{"int":`,
		`{"in":7,"ints":8,"intx":9}`,
		`{"INT":7,"Int8":-1}`,
		`{"":1,"int":2}`,
		// Scalars the inline fast paths decline, and their neighbours.
		`{"bool":tru}`,
		`{"bool":truex}`,
		`{"int":-0,"int8":-128,"int8":127,"uint8":255,"uint8":256}`,
		`{"int32":2147483647,"int32":2147483648}`,
		`{"int32":-2147483648,"int32":-2147483649}`,
		`{"uint32":4294967295,"uint32":4294967296}`,
		`{"int64":9223372036854775807,"uint64":9223372036854775808}`,
		`{"int":1234567890123456789012}`,
		`{"float64":-0,"float64":0.1,"float64":40.8,"float32":0.1,"float32":16777216.5}`,
		`{"float64":1.,"float64":1}`,
		`{"float64":01}`,
		`{"float64":123456789012345678901.5e-3}`,
		`{"string":"\u00e9","string":"\ud83d","string":"a\/b"}`,
	}
	for _, in := range inputs {
		unmarshalParity(t, "scalars", in, scalarsRef)
	}
}

func TestUnmarshalComposites(t *testing.T) {
	inputs := []string{
		`{}`,
		`{"bytes":"AAECzQ==","named_flags":"aGVsbG8=","byte_array":[1,2,3,4],"int_array":[1,2,3]}`,
		`{"int_array":[1]}`,
		`{"int_array":[1,2,3,4,5]}`,
		`{"ints":[1,2,3],"strings":["a","b"],"nested":[[1],[],null]}`,
		`{"ints":[]}`,
		`{"ints":null}`,
		`{"inners":[{"id":1},{"id":2,"note":"x"}],"inner_ptrs":[null,{"id":3}]}`,
		`{"string_map":{"a":"1","b":"2"},"inner_map":{"k":{"id":9}},"named_map":{"z":26},"color_map":{"red":1}}`,
		`{"inner":{"id":1},"inner_ptr":{"id":2},"deep_ptr":{"id":3}}`,
		`{"any":{"a":[1,2,{"b":null}],"c":true},"anys":[1,"two",null]}`,
		`{"raw":{"x":[1,2]},"number":"123","time":"2023-11-14T22:13:20Z","time_ptr":null,"dur":1000000000}`,
		`{"raw":null}`,
		`{"time":"not a time"}`,
		`{"bytes":"!!!!"}`,
		`{"ints":[1,"x"]}`,
		`{"color_map":{"red":"x"}}`,
	}
	for _, in := range inputs {
		unmarshalParity(t, "composites", in, compositesRef)
	}
}

func TestUnmarshalReusesExistingValue(t *testing.T) {
	in := []byte(`{"ints":[9],"inner_ptr":{"note":"kept"}}`)
	var a, b Composites
	seed := func(v *Composites) {
		v.Ints = []int{1, 2, 3}
		v.InnerPtr = &Inner{ID: 42}
	}
	seed(&a)
	seed(&b)
	if err := json.Unmarshal(in, compositesRef(&a)); err != nil {
		t.Fatal(err)
	}
	if err := b.UnmarshalJSON(in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("reuse mismatch:\n encoding/json: %#v\n odjson:        %#v", a, b)
	}
}

func TestUnmarshalRecursive(t *testing.T) {
	unmarshalParity(t, "recursive", `{"name":"r","children":[{"name":"a"},{"name":"b","children":[{"name":"c"}]}]}`, recursiveRef)
}

func TestRoundTrip(t *testing.T) {
	for name, v := range scalarCases() {
		b, err := v.MarshalJSON()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got Scalars
		if err := got.UnmarshalJSON(b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		v.Skipped = ""
		if !reflect.DeepEqual(v, got) {
			t.Errorf("%s round trip mismatch:\n want %#v\n got  %#v", name, v, got)
		}
	}
}
