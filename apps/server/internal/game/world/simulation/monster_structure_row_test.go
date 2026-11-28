/*
===========================================================================

monster_structure_row_test.go - a structure's standing skill in its
create row

===========================================================================
*/

package simulation

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestStructureCreateRowCarriesItsStandingSkill

The structure row's shared block carries the actor's skill list as a
monster's does: empty, the row is the one shipped before; with a
standing skill attached, the count names it and the skill and token
words follow, the name tail unmoved.
================
*/
func TestStructureCreateRowCarriesItsStandingSkill(t *testing.T) {
	spawn := Spawn{RegionID: 0x62aa, X: 100, Y: 20, Z: 100}
	base := MonsterDef{RefObjID: 19536, Structure: true, CurrentHP: 500, Name: "STRUCTURE",
		WalkSpeed: 1, RunSpeed: 2, ScaleDenom: 100}
	plain := BuildMonsterCreateRow(base, 400001, spawn)
	var effects *monster.EffectSnapshot
	effects = effects.With(monster.AttachedSkill{SkillID: 20530, Token: 77})
	attached := base
	attached.LinkedEffects = effects
	shown := BuildMonsterCreateRow(attached, 400001, spawn)
	if len(shown) != len(plain)+8 {
		t.Fatalf("attached row is %d bytes, plain %d: the skill words are missing", len(shown), len(plain))
	}
	// The rows agree until the count byte; the attached row then reads
	// count 1, the skill and the token, and the shared tail.
	diff := 0
	for diff < len(plain) && plain[diff] == shown[diff] {
		diff++
	}
	if shown[diff] != 1 || plain[diff] != 0 {
		t.Fatalf("count bytes at %d: attached %d, plain %d", diff, shown[diff], plain[diff])
	}
	if got := binary.LittleEndian.Uint32(shown[diff+1:]); got != 20530 {
		t.Fatalf("attached skill = %d, want 20530", got)
	}
	if got := binary.LittleEndian.Uint32(shown[diff+5:]); got != 77 {
		t.Fatalf("attached token = %d, want 77", got)
	}
	if !bytes.Equal(plain[diff+1:], shown[diff+9:]) {
		t.Fatal("the rows diverge after the skill words")
	}
}
