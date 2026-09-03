package ojs

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"testing/quick"
)

// roundTripJobArgs marshals a job decoded from the given args array and returns
// the args array that came back out.
func roundTripJobArgs(t *testing.T, argsJSON string) []any {
	t.Helper()

	var job Job
	in := fmt.Sprintf(`{"id":"j1","type":"a.job","args":%s}`, argsJSON)
	if err := json.Unmarshal([]byte(in), &job); err != nil {
		t.Fatalf("Unmarshal(%s): %v", in, err)
	}

	out, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got struct {
		Args []any `json:"args"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	return got.Args
}

// TestPropertyOnlySingleObjectArrayBecomesArgsMap is the rule: exactly one
// object, and nothing else, maps to the Args map form.
func TestPropertyOnlySingleObjectArrayBecomesArgsMap(t *testing.T) {
	cases := []struct {
		name      string
		wire      []any
		canonical bool
	}{
		{"single object", []any{map[string]any{"to": "a"}}, true},
		{"single empty object", []any{map[string]any{}}, true},
		{"object then scalar", []any{map[string]any{"to": "a"}, 2}, false},
		{"object then object", []any{map[string]any{"to": "a"}, map[string]any{"cc": "b"}}, false},
		{"object then null", []any{map[string]any{"to": "a"}, nil}, false},
		{"single scalar", []any{"a"}, false},
		{"single array", []any{[]any{1, 2}}, false},
		{"single null", []any{nil}, false},
		{"scalars", []any{1, "two", true}, false},
		{"scalar then object", []any{1, map[string]any{"to": "a"}}, false},
		{"empty", []any{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, canonical := canonicalObjectArgs(tc.wire)
			if canonical != tc.canonical {
				t.Fatalf("canonicalObjectArgs(%v) = %v, want %v", tc.wire, canonical, tc.canonical)
			}
			// An empty array is neither canonical nor positional: there is
			// nothing to preserve, so Args round-trips it.
			wantPositional := !tc.canonical && len(tc.wire) > 0
			if got := isPositionalWireArgs(tc.wire); got != wantPositional {
				t.Fatalf("isPositionalWireArgs(%v) = %v, want %v", tc.wire, got, wantPositional)
			}

			args := argsFromWire(tc.wire)
			if tc.canonical {
				// Canonical args alias the decoded object.
				obj, _ := tc.wire[0].(map[string]any)
				if len(args) != len(obj) {
					t.Fatalf("argsFromWire(%v) = %v, want the object itself", tc.wire, args)
				}
				return
			}
			if len(args) != len(tc.wire) {
				t.Fatalf("argsFromWire(%v) = %v, want one indexed entry per element", tc.wire, args)
			}
			for i := range tc.wire {
				if !reflect.DeepEqual(args[fmt.Sprintf("%d", i)], tc.wire[i]) {
					t.Fatalf("argsFromWire(%v)[%d] = %v, want %v", tc.wire, i, args[fmt.Sprintf("%d", i)], tc.wire[i])
				}
			}
		})
	}
}

// TestPropertyMultiElementArgsRoundTripLosslessly is the regression the rule
// exists for: an object-leading multi-element array used to collapse to its
// first element and lose every argument after it.
func TestPropertyMultiElementArgsRoundTripLosslessly(t *testing.T) {
	f := func(key, val string, trailing int, flag bool) bool {
		if key == "" {
			return true
		}
		leading, err := json.Marshal(map[string]string{key: val})
		if err != nil {
			return true
		}
		argsJSON := fmt.Sprintf(`[%s,%d,%t]`, leading, trailing, flag)

		var job Job
		if err := json.Unmarshal([]byte(fmt.Sprintf(
			`{"id":"j1","type":"a.job","args":%s}`, argsJSON)), &job); err != nil {
			return false
		}
		if len(job.RawArgs) != 3 {
			return false
		}

		out, err := json.Marshal(job)
		if err != nil {
			return false
		}
		var got struct {
			Args []any `json:"args"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			return false
		}
		if len(got.Args) != 3 {
			return false
		}
		obj, ok := got.Args[0].(map[string]any)
		if !ok || obj[key] != val {
			return false
		}
		n, ok := got.Args[1].(float64)
		if !ok || n != float64(trailing) {
			return false
		}
		return got.Args[2] == flag
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// TestPropertyArgsWireBytesArePreserved checks the strongest form of the
// property: for any args array the SDK did not build itself, the bytes that go
// out equal the bytes that came in.
func TestPropertyArgsWireBytesArePreserved(t *testing.T) {
	cases := []string{
		`[]`,
		`[{"to":"a@example.com"}]`,
		`[{}]`,
		`[1,"two",true]`,
		`[{"a":1},2,3]`,
		`[{"a":1},{"b":2}]`,
		`[{"a":1},null]`,
		`["a",{"b":2}]`,
		`[[1,2],[3,4]]`,
		`[null]`,
		`[{"nested":{"deep":[1,2,3]}},"tail"]`,
	}

	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			var want []any
			if err := json.Unmarshal([]byte(in), &want); err != nil {
				t.Fatalf("fixture is not valid JSON: %v", err)
			}
			got := roundTripJobArgs(t, in)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round-tripped args = %#v, want %#v", got, want)
			}
		})
	}
}

// TestPropertyPositionalArgsIndexViewIsComplete locks the convenience view: for
// a positional array every element is reachable by its index key, so nothing is
// invisible to a handler.
func TestPropertyPositionalArgsIndexViewIsComplete(t *testing.T) {
	f := func(elems []string) bool {
		if len(elems) < 2 {
			return true
		}
		wire := make([]any, 0, len(elems)+1)
		wire = append(wire, map[string]any{"first": true})
		for _, e := range elems {
			wire = append(wire, e)
		}

		args := argsFromWire(wire)
		if len(args) != len(wire) {
			return false
		}
		if _, ok := args["0"].(map[string]any); !ok {
			return false
		}
		for i, e := range elems {
			if args[fmt.Sprintf("%d", i+1)] != e {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// TestPropertySingleObjectArgsStayAuthoritative keeps the canonical form's
// documented behaviour: Args is the source of truth, so handler mutations
// still reach the wire.
func TestPropertySingleObjectArgsStayAuthoritative(t *testing.T) {
	f := func(key, oldVal, newVal string) bool {
		if key == "" || oldVal == newVal {
			return true
		}
		encoded, err := json.Marshal(map[string]string{key: oldVal})
		if err != nil {
			return true
		}

		var job Job
		if err := json.Unmarshal([]byte(fmt.Sprintf(
			`{"id":"j1","type":"a.job","args":[%s]}`, encoded)), &job); err != nil {
			return false
		}
		job.Args[key] = newVal

		out, err := json.Marshal(job)
		if err != nil {
			return false
		}
		var got struct {
			Args []any `json:"args"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			return false
		}
		if len(got.Args) != 1 {
			return false
		}
		obj, ok := got.Args[0].(map[string]any)
		return ok && obj[key] == newVal
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// TestPropertyArgsToWireIsAlwaysCanonical checks the producing direction: Args
// built in Go always serialise to the canonical single-object array.
func TestPropertyArgsToWireIsAlwaysCanonical(t *testing.T) {
	f := func(key string, val float64) bool {
		if key == "" || math.IsNaN(val) || math.IsInf(val, 0) {
			return true
		}
		wire := argsToWire(Args{key: val})
		if _, canonical := canonicalObjectArgs(wire); !canonical {
			return false
		}
		return argsFromWire(wire)[key] == val
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}
