package enterworld

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

func TestNativeFieldsPresenceBitsAndImmutableUpdates(t *testing.T) {
	values := make(map[string]float64)
	for i, key := range nativeFieldNames {
		if i%3 != 0 {
			values[key] = math.Float64frombits(uint64(i) << 40)
		}
	}
	values["canUse"] = 0
	values["sellPrice"] = math.Copysign(0, -1)
	f := NewNativeFields(values)
	for _, key := range nativeFieldNames {
		want, present := values[key]
		got, ok := f.Lookup(key)
		if present != ok || math.Float64bits(want) != math.Float64bits(got) {
			t.Fatalf("lost field %s", key)
		}
	}
	a, _ := json.Marshal(values)
	b, err := json.Marshal(f)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("wire object changed")
	}
	var decoded NativeFields
	if err := json.Unmarshal(b, &decoded); err != nil || decoded != f {
		t.Fatal("JSON roundtrip changed fields")
	}
	changed := f.With("canUse", 129).Without("sellPrice")
	if f.Get("canUse") != 0 || changed.Get("canUse") != 129 {
		t.Fatal("update mutated published source")
	}
	if _, ok := changed.Lookup("sellPrice"); ok {
		t.Fatal("absent field became zero")
	}
	if _, ok := f.With("canUse", math.NaN()).Lookup("canUse"); !ok {
		t.Fatal("invalid numeric value became absent")
	}
	if _, err := json.Marshal(f.With("canUse", math.NaN())); err == nil {
		t.Fatal("nonfinite JSON accepted")
	}
}

/*
================
TestNativeFieldsJSONMatchesTheMapEncoding

The direct writer must stay byte-identical to json.Marshal of the same
values as a map (#2143): every magnitude band encoding/json formats
differently, signs, zero, negative zero, and the empty projection.
================
*/
func TestNativeFieldsJSONMatchesTheMapEncoding(t *testing.T) {
	samples := []float64{0, math.Copysign(0, -1), 1, -1, 129, 250, 0.5, -2.25, 1.0 / 3, 1e-6, 9.99e-7, 1e-7, 1.5e-12,
		1e20, 1e21, 1.5e21, -3e25, 123456789012345, math.MaxFloat64, math.SmallestNonzeroFloat64, 4294967295, 0.1 + 0.2}
	for start := 0; start < len(samples); start++ {
		values := make(map[string]float64)
		for i, key := range nativeFieldNames {
			if (i+start)%4 != 0 {
				values[key] = samples[(i+start)%len(samples)]
			}
		}
		want, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(NewNativeFields(values))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("sample offset %d: got %s, want %s", start, got, want)
		}
	}
	if got, err := json.Marshal(NativeFields("")); err != nil || string(got) != "{}" {
		t.Fatalf("empty projection wrote %s, %v", got, err)
	}
	if _, err := json.Marshal(NewNativeFields(map[string]float64{"canUse": math.Inf(-1)})); err == nil {
		t.Fatal("infinite JSON accepted")
	}
}

/*
================
BenchmarkNativeFieldsJSON

One item row's field projection as the entry blob writes it.
================
*/
func BenchmarkNativeFieldsJSON(b *testing.B) {
	values := make(map[string]float64)
	for i, key := range nativeFieldNames {
		if i%5 == 0 {
			values[key] = float64(i * 37)
		}
	}
	f := NewNativeFields(values)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := f.MarshalJSON(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNativeFieldsLookup(b *testing.B) {
	values := map[string]float64{"canUse": 129, "maxStack": 250, "sellPrice": 42}
	compact := NewNativeFields(values)
	b.Run("map", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if values["maxStack"] != 250 {
				b.Fatal("value")
			}
		}
	})
	b.Run("compact", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if compact.Get("maxStack") != 250 {
				b.Fatal("value")
			}
		}
	})
}
