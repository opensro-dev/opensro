/*
===========================================================================

hitretirement_test.go - shared native hit consequences across actor kinds

Root depends on magical damage, including a weapon imbue. Sleep and Stun
follow the execution selector rather than the final saturated HP debit.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetHitRetiresStatusesByDamageLane
================
*/
func TestPetHitRetiresStatusesByDamageLane(t *testing.T) {
	for _, flags := range []uint32{5, 9, 13} {
		rt, clock, c, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, c, 2)
		source.Ref.DefaultSkillIDs[0] = 2
		skills := rt.deps.SkillData().(staticSkillSource)
		row := skills[2]
		row.Attack.Min, row.Attack.Max, row.Attack.Percent, row.Attack.Flags = 1, 1, 100, flags
		row.ReplacementPinned, row.Replacement.MatchesExecutionSelector = true, true
		skills[2] = row
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		now := clock.NowMs()
		var records []abnormal.Record
		for _, status := range []abnormal.Status{abnormal.Root, abnormal.Sleep, abnormal.Stun} {
			records = append(records, abnormal.Record{Status: status, Grade: 1, DurationMs: 10000, SourceGID: source.Gid})
		}
		owner := rt.newCosAbnormalOwner(testDivision, c, now)
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, records)
		for _, record := range records {
			if !owner.block.Apply(owner, record, now) {
				t.Fatal("pet status refused")
			}
		}
		owner.commit()
		result := rt.MonsterBasicAttack(testDivision, source, c.ActiveCOS.GID, 2, now)
		mask := uint32(0)
		if block := rt.cosAbnormal(testDivision, c.Name, c.ActiveCOS.GID); block != nil {
			mask = block.Mask
		}
		want := uint32(0)
		if flags&8 == 0 {
			want = abnormal.Root.Bit()
		}
		if !result.Accepted || mask != want {
			t.Fatalf("flags %x: accepted %v mask %x want %x", flags, result.Accepted, mask, want)
		}
	}
}

/*
================
TestMonsterRootRetirementIncludesWeaponImbue
================
*/
func TestMonsterRootRetirementIncludesWeaponImbue(t *testing.T) {
	for _, imbued := range []bool{false, true} {
		rt, clock, c, target := newCombatTestRuntime(t, 100000)
		if imbued {
			row := installFireImbue(t, rt, c)
			result := castSelf(rt, c, row.ID)
			if result.DiagnosticRefusal != "" {
				t.Fatalf("imbue activation: %+v", result)
			}
		}
		skills := rt.deps.SkillData().(staticSkillSource)
		row := skills[2]
		row.ReplacementPinned, row.Replacement.MatchesExecutionSelector = true, true
		skills[2] = row
		records := []abnormal.Record{{Status: abnormal.Root, Grade: 1, DurationMs: 10000,
			SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}}
		plans := []simulation.MonsterDamagePlan{{GID: target.Gid, Abnormal: records,
			AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, records)}}
		if len(rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, plans)) != 1 {
			t.Fatal("monster root refused")
		}
		result := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
		after, exists := rt.Monsters.Get(testDivision, target.Gid)
		rooted := after.Abnormal != nil && after.Abnormal.Has(abnormal.Root)
		if result.DiagnosticRefusal != "" || !exists || after.CurrentHP >= target.CurrentHP || rooted == imbued {
			t.Fatalf("imbued %v: rooted %v hp %d at %d result %+v", imbued, rooted, after.CurrentHP, clock.NowMs(), result)
		}
	}
}
