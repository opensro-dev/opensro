/*
===========================================================================

nativegroundstep_test.go - finite ground steps against original server bytes

The shared corpus executes 48C1B0 and 48BFF0 in Unicorn. Compare stored
float bits, including per-component cutoffs and the elapsed-distance cap.
The corpus does not establish full-world collision equivalence.

===========================================================================
*/
package simulation

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

/*
================
TestNativeGroundStepReference
================
*/
func TestNativeGroundStepReference(t *testing.T) {
	data, err := os.ReadFile("testdata/native-mover-step-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Format string `json:"format"`
		Cases  []struct {
			Name           string     `json:"name"`
			Speed          float32    `json:"speed"`
			ElapsedSeconds float32    `json:"elapsedSeconds"`
			Direction      [2]float32 `json:"direction"`
			StepBits       [3]uint32  `json:"stepBits"`
			Accepted       bool       `json:"accepted"`
		} `json:"cases"`
		ClampCases []struct {
			RequestedStep [3]float32 `json:"requestedStep"`
			Remaining     [3]float32 `json:"remaining"`
			StepBits      [3]uint32  `json:"stepBits"`
		} `json:"clampCases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Format != "sro-native-mover-step-v1" || len(corpus.Cases) == 0 {
		t.Fatalf("invalid native movement corpus: format %q, cases %d", corpus.Format, len(corpus.Cases))
	}
	for _, row := range corpus.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if !row.Accepted {
				t.Fatal("active fixture did not enter native step computation")
			}
			x, z := NativeGroundStep(row.Speed, row.ElapsedSeconds, row.Direction[0], row.Direction[1])
			got := [3]uint32{math.Float32bits(x), 0, math.Float32bits(z)}
			if got != row.StepBits {
				t.Fatalf("step bits %08x, native %08x (speed %g, seconds %g, direction %v)",
					got, row.StepBits, row.Speed, row.ElapsedSeconds, row.Direction)
			}
		})
	}
	if len(corpus.ClampCases) == 0 {
		t.Fatal("missing native destination-clamp cases")
	}
	for index, row := range corpus.ClampCases {
		step := NativeGroundDestination(row.RequestedStep, row.Remaining)
		got := [3]uint32{math.Float32bits(step[0]), math.Float32bits(step[1]), math.Float32bits(step[2])}
		if got != row.StepBits {
			t.Fatalf("clamp %d bits %08x, native %08x (requested %v, remaining %v)",
				index, got, row.StepBits, row.RequestedStep, row.Remaining)
		}
	}
}
