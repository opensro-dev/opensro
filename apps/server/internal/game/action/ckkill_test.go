/*
===========================================================================

ckkill_test.go - the ck parameter's outright kill (58F74E)

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCkKillSlaysWithoutDamage

An unblocked ck impact at chance 100 is record 0x86: the player dies with
no damage on the wire, and the kill wears no armour.
================
*/
func TestCkKillSlaysWithoutDamage(t *testing.T) {
	rt, clock, c, monster := newCombatTestRuntime(t, 100)
	monster.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	hit := skills[2]
	hit.Attack.Min, hit.Attack.Max, hit.Attack.Percent = 1, 1, 100
	hit.Ck, hit.CkChance = true, 100
	skills[2] = hit
	rt.WearRoll = func() (uint32, error) {
		t.Fatal("a ck kill rolled armour wear")
		return 0, nil
	}
	gid := enterworld.ObjectIDForCharacter(c)
	r := rt.MonsterBasicAttack(testDivision, monster, gid, 2, clock.NowMs())
	if !r.Accepted || r.TargetAlive || enterworld.CharacterAlive(c) {
		t.Fatalf("ck hit %+v left hp %d", r, enterworld.CurrentHP(c))
	}
	record := binary.LittleEndian.AppendUint32(nil, gid)
	record = append(record, 0x86)
	for _, frame := range r.Frames {
		if at := bytes.Index(frame.Payload, record); at >= 0 && at+len(record) == len(frame.Payload) {
			return
		}
	}
	t.Fatalf("no bare 0x86 record for %d in %+v", gid, r.Frames)
}
