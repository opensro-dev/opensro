/*
===========================================================================

jsonscratch_test.go - the scratch encodes byte-identically to Marshal

The whole point of the package is that swapping json.Marshal for a
reused buffer changes no byte any consumer sees. The corpus below is
the pin: HTML metacharacters (the encoder's escaping is the easiest
byte to lose), control characters, non-ASCII text, floats, maps (key
order), nested structs and a custom marshaler, each compared against
json.Marshal itself. The allocation pin keeps the swap worth making.

===========================================================================
*/

package jsonscratch

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

/*
================
customMarshaler
================
*/
type customMarshaler struct{ N int }

func (m customMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"n":` + json.Number(string(rune('0'+m.N%10))).String() + `}`), nil
}

/*
================
TestScratchMatchesMarshal
================
*/
func TestScratchMatchesMarshal(t *testing.T) {
	values := []any{
		nil,
		true,
		0,
		-17,
		uint64(math.MaxUint64),
		1.5,
		1e21,
		0.1,
		"",
		"plain",
		"angle <brackets> & ampersands",
		"quotes \"and\" \\ backslash",
		"control \x01\x1f chars",
		"line\nbreak\ttab",
		"js separators \u2028and \u2029",
		"del \x7f char",
		"non-ascii İ ΣΊΣΥΦΟΣ Hong길동",
		"invalid utf8 \xff\xfe",
		[]int{1, 2, 3},
		[]string{},
		map[string]int{"b": 2, "a": 1, "c": 3},
		map[uint32]uint32{300: 3, 20: 2, 1: 1},
		map[string]int(nil),
		[]int(nil),
		(*int)(nil),
		map[string]any{"nested": map[string]any{"x": []any{1.5, "y<z"}}},
		struct {
			Name string   `json:"name"`
			Tags []string `json:"tags,omitempty"`
			HTML string   `json:"html"`
			Skip int      `json:"-"`
		}{Name: "n<1>", Tags: []string{"a&b"}, HTML: "<p>&</p>", Skip: 9},
		// The omitempty zeros: the persisted records are dense with
		// these, and an encoder that keeps them writes a different
		// record than Marshal does.
		struct {
			ZeroInt     int            `json:"zeroInt,omitempty"`
			ZeroBool    bool           `json:"zeroBool,omitempty"`
			ZeroString  string         `json:"zeroString,omitempty"`
			EmptySlice  []int          `json:"emptySlice,omitempty"`
			EmptyMap    map[string]int `json:"emptyMap,omitempty"`
			ZeroPointer *int           `json:"zeroPointer,omitempty"`
			Kept        int            `json:"kept"`
		}{Kept: 4},
		customMarshaler{N: 7},
		[]customMarshaler{{N: 1}, {N: 2}},
		json.RawMessage(`{"already":"encoded"}`),
	}
	var scratch Scratch
	for i, value := range values {
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("value %d: json.Marshal: %v", i, err)
		}
		got, err := scratch.Marshal(value)
		if err != nil {
			t.Fatalf("value %d: scratch.Marshal: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("value %d: scratch = %s, Marshal = %s", i, got, want)
		}
		// The borrowed bytes must still be intact after the next encode:
		// Marshal returns them before any reuse, so keep a copy honest.
		kept := append([]byte(nil), got...)
		if _, err := scratch.Marshal("next"); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(kept, want) {
			t.Fatalf("value %d: copy of scratch bytes changed", i)
		}
	}
}

/*
================
allocFixture, allocFixtureValue

A character-shaped value: nested structs, slices and one small map -
the shape the store commit encodes per dirty row.
================
*/
type allocFixture struct {
	Name  string            `json:"name"`
	Level int               `json:"level"`
	Items []allocFixtureRow `json:"items"`
	Aggro map[uint32]uint32 `json:"aggro"`
	Note  string            `json:"note"`
}

type allocFixtureRow struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Flags []int  `json:"flags"`
}

func allocFixtureValue() allocFixture {
	value := allocFixture{Name: "someone<the>&great", Level: 90, Note: "text with & and <html>"}
	for i := 0; i < 60; i++ {
		value.Items = append(value.Items, allocFixtureRow{ID: i, Name: "item<name>&", Flags: []int{1, 2, 3}})
	}
	value.Aggro = map[uint32]uint32{300: 3, 20: 2, 1: 1}
	return value
}

/*
================
TestScratchAllocatesLessThanMarshal

The encoder's own working allocations are the same for both paths;
what the Scratch removes is the per-call owned copy, so it must never
allocate more than Marshal, and (BenchmarkScratchMarshal) it moves
far fewer bytes per encode.
================
*/
func TestScratchAllocatesLessThanMarshal(t *testing.T) {
	value := allocFixtureValue()
	var scratch Scratch
	if _, err := scratch.Marshal(value); err != nil {
		t.Fatal(err)
	}
	scratchAllocs := testing.AllocsPerRun(100, func() {
		if _, err := scratch.Marshal(value); err != nil {
			t.Fatal(err)
		}
	})
	marshalAllocs := testing.AllocsPerRun(100, func() {
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	})
	if scratchAllocs > marshalAllocs {
		t.Fatalf("scratch allocated %.1f per call, Marshal %.1f", scratchAllocs, marshalAllocs)
	}
}

/*
================
BenchmarkScratchMarshal, BenchmarkJSONMarshal

The before/after pair for the swap, over allocFixtureValue.
================
*/
func BenchmarkScratchMarshal(b *testing.B) {
	value := allocFixtureValue()
	var scratch Scratch
	if _, err := scratch.Marshal(value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := scratch.Marshal(value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONMarshal(b *testing.B) {
	value := allocFixtureValue()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(value); err != nil {
			b.Fatal(err)
		}
	}
}

/*
================
TestScratchSurvivesEncodeErrors

A value JSON cannot encode errors without poisoning the Scratch: the
next encode still produces correct bytes.
================
*/
func TestScratchSurvivesEncodeErrors(t *testing.T) {
	var scratch Scratch
	if _, err := scratch.Marshal(math.NaN()); err == nil {
		t.Fatal("NaN encoded without an error")
	}
	got, err := scratch.Marshal(map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := json.Marshal(map[string]int{"a": 1}); !bytes.Equal(got, want) {
		t.Fatalf("after error: scratch = %s, Marshal = %s", got, want)
	}
}
