package monster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestFrozenHomingNativeVectors(t *testing.T) {
	var corpus struct {
		CandidateSHA256 string
		Cases           []struct {
			Op            string
			Home, Current [3]float64
			Radius        float32
			Draw          uint32
			Exact         bool
			Expected      struct {
				Bits                           [3]uint32
				Draws, Requests, Latch, Result uint32
			}
		}
	}
	data, err := os.ReadFile("testdata/ai-homing-help-native-v1188.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("homing_destination.go")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	if hex.EncodeToString(hash[:]) != corpus.CandidateSHA256 {
		t.Fatal("homing candidate changed since native corpus freeze")
	}
	for i, c := range corpus.Cases {
		if c.Op == "help" {
			// The executed native help chain never produces a negative
			// band. Enabling data bytes alone must not invent calls.
			if c.Expected.Requests != 0 || c.Expected.Latch != 0 || c.Expected.Result != 1 {
				t.Fatalf("native automatic help %d: %+v", i, c.Expected)
			}
			continue
		}
		ref := MonsterRef{TidWord: 0xc6}
		if c.Exact {
			ref = MonsterRef{TidWord: 0x246, TypeID4: 4}
		}
		draws := uint32(0)
		got := HomingCandidate(Pose{RegionID: 25000, X: c.Home[0], Y: c.Home[1], Z: c.Home[2]}, Pose{RegionID: 25000, X: c.Current[0], Y: c.Current[1], Z: c.Current[2]}, c.Radius, ref, func() uint32 { draws++; return c.Draw })
		bits := [3]uint32{math.Float32bits(float32(got.X)), math.Float32bits(float32(got.Y)), math.Float32bits(float32(got.Z))}
		if bits != c.Expected.Bits || draws != c.Expected.Draws {
			t.Fatalf("native case %d: %+v got %#v draws %d", i, c, bits, draws)
		}
	}
}
