package monster

import (
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestRecoveredSixteenOwnTacticsEveryGrade(t *testing.T) {
	licensed.RequireGameData(t)
	refs := LoadMonsterRefs(gamedatatest.TextdataDir(t))
	var doc supplementalSummonTactics
	if err := json.Unmarshal(supplementalSummonTacticsJSON, &doc); err != nil {
		t.Fatal(err)
	}
	bindings := 0
	for _, row := range doc.Rows {
		ref, ok := refs[row.PrimaryRefID]
		if !ok || ref.Codename != row.Codename {
			t.Fatalf("target identity lost: %s", row.Codename)
		}
		for grade := 0; grade < 256; grade++ {
			got, found := ResolveSummonTactics(ref, uint8(grade), func() float64 { t.Fatal("singleton tactics consumed entropy"); return 0 })
			want := row.Normal[0]
			if grade&15 != 0 {
				want = row.Champion[0]
			}
			if !found || !got.HasControls || got.Controls != want || got.SightRange != float64(float32(float64(want.SightRange)+ref.BodyRadius)) || got.NativeFlags != want.Flags || got.TargetPolicy != want.ChangeTarget {
				t.Fatalf("%s grade %d lost own complete tactics: %+v", row.Codename, grade, got)
			}
			n := 0
			for _, binding := range row.ConditionalSkills {
				if binding.TacticsID == want.ID {
					if got.ConditionalSkills[n] != binding {
						t.Fatal("conditional binding lost")
					}
					n++
				}
			}
			for _, binding := range got.ConditionalSkills[n:] {
				if binding.SkillID != 0 {
					t.Fatal("borrowed conditional binding")
				}
			}
		}
		bindings += len(row.ConditionalSkills)
	}
	if len(doc.Rows) != 16 || bindings != 8 {
		t.Fatalf("source closure: references=%d bindings=%d", len(doc.Rows), bindings)
	}
}
