package combat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func TestDefenseModifierNativeProducer(t *testing.T) {
	b, err := os.ReadFile("testdata/native-defense-writer-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Schema    string
		Candidate string `json:"candidate_sha256"`
		Cases     []struct {
			Physical, Magical uint32
			PhysicalBonus     uint32  `json:"physical_bonus"`
			MagicalBonus      uint32  `json:"magical_bonus"`
			Cap               uint32  `json:"cap_percent"`
			CurrentPhysical   float32 `json:"current_physical"`
			CurrentMagical    float32 `json:"current_magical"`
			Writes            []struct {
				Parameter uint16
				Channel   uint8
				Source    uint32
				Bits      uint32 `json:"float32_bits"`
			}
		}
	}
	if err = json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != "sro-native-defense-writer-v2" || len(report.Cases) != 264 {
		t.Fatal("invalid native observations")
	}
	source, err := os.ReadFile("defensemodifier.go")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	if hex.EncodeToString(hash[:]) != report.Candidate {
		t.Fatal("candidate changed after native observations; rerun frozen-candidate comparison")
	}
	for i, c := range report.Cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got, err := defenseModifierWrites(0x3002000, DefenseModifierInput{Physical: c.Physical, Magical: c.Magical, PhysicalBonus: c.PhysicalBonus, MagicalBonus: c.MagicalBonus, CapPercent: c.Cap, CurrentPhysical: c.CurrentPhysical, CurrentMagical: c.CurrentMagical})
			if err != nil || len(got) != len(c.Writes) {
				t.Fatalf("writes %v, err %v", got, err)
			}
			for j, w := range c.Writes {
				g := got[j]
				if g.Parameter != w.Parameter || uint8(g.Channel) != w.Channel || g.Source != w.Source || math.Float32bits(g.Value) != w.Bits {
					t.Fatalf("write %d: port %+v bits %x; native %+v", j, g, math.Float32bits(g.Value), w)
				}
			}
		})
	}
}

func TestDefenseCapRequiresValidInstallationSnapshot(t *testing.T) {
	for _, v := range []float32{-1, 10000000, float32(math.Inf(1)), float32(math.NaN())} {
		if _, err := defenseModifierWrites(123, DefenseModifierInput{CapPercent: 10, CurrentPhysical: v}); err == nil {
			t.Fatalf("invalid cap snapshot accepted %v", v)
		}
	}
	// No cap means no read of recipient defense at all, as in the server's installer.
	if _, err := defenseModifierWrites(123, DefenseModifierInput{Physical: 20, CurrentPhysical: float32(math.NaN())}); err != nil {
		t.Fatal(err)
	}
}
