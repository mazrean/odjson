package crosspkg

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mazrean/odjson/internal/testfixture/other"
)

func TestMarshalHolder(t *testing.T) {
	cases := map[string]Holder{
		"zero": {},
		"full": {
			Thing:   other.Thing{ID: 1, Name: "a"},
			Ptr:     &other.Thing{ID: 2},
			Wrapper: other.Wrapper{Thing: other.Thing{ID: 3}, Ptr: &other.Thing{ID: 4, Name: "d"}, List: []other.Thing{{ID: 5}}},
			List:    []other.Thing{{ID: 6}, {ID: 7, Name: "g"}},
			Map:     map[string]other.Thing{"b": {ID: 9}, "a": {ID: 8}},
			Deep:    []*other.Wrapper{nil, {Thing: other.Thing{ID: 10}}},
		},
	}
	for name, v := range cases {
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := AppendHolder(nil, &v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
		}
	}
}

func TestUnmarshalHolder(t *testing.T) {
	for _, in := range []string{
		`{}`,
		`{"thing":{"id":1,"name":"a"},"ptr":{"id":2},"ptr2":null}`,
		`{"wrapper":{"thing":{"id":3},"ptr":null,"list":[{"id":4}]}}`,
		`{"list":[{"id":1},{"id":2}],"map":{"a":{"id":3}}}`,
		`{"deep":[null,{"thing":{"id":5}}]}`,
		`{"ptr":null}`,
		`{"thing":"nope"}`,
	} {
		var a, b Holder
		errA := json.Unmarshal([]byte(in), &a)
		errB := UnmarshalHolder([]byte(in), &b)
		if (errA != nil) != (errB != nil) {
			t.Errorf("error mismatch for %s: encoding/json=%v odjson=%v", in, errA, errB)
			continue
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Errorf("value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", in, a, b)
		}
	}
}
