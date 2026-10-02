package statuseffect

import (
	"fmt"
	"opensro.online/server/internal/game/paramkeeper"
	"os"
	"strings"
	"testing"
)

func TestRetirementSelectionAgainstNative(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-retirement-selector.txt")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(rows) != 512 {
		t.Fatalf("case count %d", len(rows))
	}
	for _, line := range rows {
		var force, flag, current, category, a, b, cbuf, want uint32
		if n, err := fmt.Sscan(line, &force, &flag, &current, &category, &a, &b, &cbuf, &want); err != nil || n != 8 {
			t.Fatal(line, err)
		}
		if got := RetirementSelected(force != 0, uint8(flag), current != 0, category, a != 0, b != 0, cbuf != 0); got != (want != 0) {
			t.Fatal("native mismatch", line, got)
		}
	}
}

func TestDeathRetiresModifierOwnershipButPreservesProtectedEffects(t *testing.T) {
	r := NewRegistry()
	m, err := NewModifiers([]paramkeeper.Write{{Parameter: 5, Value: 20}})
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(1); i <= 3; i++ {
		if !r.Apply(Effect{DivisionID: "d", CharacterName: "c", SkillID: i, SkillGroup: i, InstanceToken: i, Modifiers: m, DeathProtected: i == 2, Persistent: i == 3}) {
			t.Fatal("install", i)
		}
	}
	before := r.ModifierWrites("d", "c")
	ended := r.RetireBodyStatusesOnDeath("d", "c")
	if len(ended) != 1 || ended[0].InstanceToken != 1 {
		t.Fatal(ended)
	}
	after := r.ModifierWrites("d", "c")
	if len(after) != 2 || after[0].Source != before[1].Source || after[1].Source != before[2].Source {
		t.Fatal("surviving ownership changed", after)
	}
	if len(r.RetireBodyStatusesOnDeath("d", "c")) != 0 {
		t.Fatal("duplicate retirement")
	}
}

/*
================
TestDeathRetiresPresentationOnlyEffects

A buff that installs no modifier, status or link (a planted trap's board
entry) still ends on death; only cbuf and durable jobs survive.
================
*/
func TestDeathRetiresPresentationOnlyEffects(t *testing.T) {
	r := NewRegistry()
	if !r.Apply(Effect{DivisionID: "d", CharacterName: "c", SkillID: 7, SkillGroup: 7, InstanceToken: 7}) {
		t.Fatal("install")
	}
	if ended := r.RetireBodyStatusesOnDeath("d", "c"); len(ended) != 1 || ended[0].InstanceToken != 7 {
		t.Fatal("presentation-only buff survived death", ended)
	}
}
