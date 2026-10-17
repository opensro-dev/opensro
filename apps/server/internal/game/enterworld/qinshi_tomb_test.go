/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb monster skills and summon references

Tests both SRO_QINSHI_TOMB off (native default) and on: 230 monster skills
mapped into the v1.150 skill ID gaps (10565..10730, 25047..25073,
30612..30647, 30686) and full foreign-key closure across all 79 Tomb monster
DefaultSkillIDs and summon references.

===========================================================================
*/

package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestQinShiTombSkillsAndMonsterDefaultSkills
================
*/
func TestQinShiTombSkillsAndMonsterDefaultSkills(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)

	t.Setenv(monster.EnvQinShiTomb, "off")
	baseSkills := NewTextdataSkills(dir)
	baseCount := baseSkills.Len()
	if baseCount == 0 {
		t.Fatal("expected non-empty base skill table")
	}
	if _, ok := baseSkills.SkillByID(10565); ok {
		t.Fatal("base v1.150 skill table must not contain Tomb skill 10565 when gate is off")
	}

	t.Setenv(monster.EnvQinShiTomb, "on")
	tombSkills := NewTextdataSkills(dir)
	if got, want := tombSkills.Len()-baseCount, 230; got != want {
		t.Fatalf("added tomb skills = %d, want %d", got, want)
	}

	baseMonsters := monster.LoadTemplate(dir)
	withTomb, err := baseMonsters.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	withSummons, err := WithMonsterSummonReferences(withTomb, tombSkills)
	if err != nil {
		t.Fatalf("WithMonsterSummonReferences: %v", err)
	}

	// Every DefaultSkillID on every Tomb monster must resolve in tombSkills.
	for id, ref := range withSummons.Refs {
		if _, inBase := baseMonsters.Refs[id]; inBase {
			continue
		}
		for _, skillID := range ref.DefaultSkillIDs {
			if skillID == 0 {
				continue
			}
			if _, ok := tombSkills.SkillByID(skillID); !ok {
				t.Fatalf("tomb monster %d (%s) references missing skill %d", id, ref.Codename, skillID)
			}
		}
	}
}
