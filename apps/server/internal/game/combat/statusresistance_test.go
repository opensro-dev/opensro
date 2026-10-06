/*
===========================================================================

statusresistance_test.go - shared passive and buff resistance selection

Compare every status bucket against original 5999E0 machine execution,
including unsigned boundaries and order-independent equal-grade stacking.

===========================================================================
*/

package combat

import (
	"encoding/json"
	"os"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestFileStatusResistanceNativeReader
================
*/
func TestFileStatusResistanceNativeReader(t *testing.T) {
	data, err := os.ReadFile("testdata/native-status-resistance.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		BinarySHA256 string
		Cases        []struct {
			Entries              [][2]uint32
			Percent, Grade, Flat uint32
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.BinarySHA256 != "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290" || len(fixture.Cases) != 2401 {
		t.Fatal("native fixture identity or scope")
	}
	for _, row := range fixture.Cases {
		for _, source := range abnormal.Sources {
			if source.Resist < 0 {
				continue
			}
			var buckets [17]abnormal.Resistance
			var filed [17]bool
			for _, entry := range row.Entries {
				FileStatusResistance(&buckets, &filed, enterworld.SkillPassiveReal{Mask: source.Status.Bit(), Grade: entry[0], Flat: entry[1]})
			}
			for i, got := range buckets {
				want := abnormal.Resistance{}
				if i == int(source.Resist) {
					want = abnormal.Resistance{Grade: int32(row.Grade), Flat: int32(row.Flat)}
				}
				if got != want {
					t.Fatalf("entries %v status %v bucket %d: %+v want %+v", row.Entries, source.Status, i, got, want)
				}
			}
		}
	}
}

/*
================
TestFileStatusResistancePreservesPercent
================
*/
func TestFileStatusResistancePreservesPercent(t *testing.T) {
	var buckets [17]abnormal.Resistance
	var filed [17]bool
	for i := range buckets {
		buckets[i].Percent = 17
	}
	FileStatusResistance(&buckets, &filed, enterworld.SkillPassiveReal{Mask: ^uint32(0), Grade: 8, Flat: 100})
	for _, got := range buckets {
		if got.Percent != 17 {
			t.Fatal(got)
		}
	}
}
