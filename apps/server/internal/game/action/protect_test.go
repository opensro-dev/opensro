/*
===========================================================================

protect_test.go - the Warrior's Protect (GUARDA_AGGRO_A)

Protect is the Cleric blessings' lnks pair with lkag instead of stat
writes: while the link holds, the protected member's aggression on a
monster is split and the Warrior's share is dispatched first (5A03A0).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	protectA1        = 7246 // SKILL_EU_WARRIOR_GUARDA_AGGRO_A_01: lnks 3 1500 2 1, dura 1800000, lkag 36 0
	protectA1Percent = 36
	protectHit       = 100
)

/*
==================
TestProtectLinksAndDivertsTheMembersAggression

A cast on an ally installs the source half on the Warrior and the
recipient half on the ally. The ally's hit on a monster then hands lkag
percent of its aggression to the Warrior, never damage, and leaving the
1500 link range retires both halves and the transfer.
==================
*/
func TestProtectLinksAndDivertsTheMembersAggression(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	learnShipped(t, rt, c, protectA1)
	ally := nearbyCharacter(rt, c, 21, "protected", 1)
	warrior, member := enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(ally)

	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: protectA1, HasTarget: true, TargetGid: member}.Encode())
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("protect refused: %+v", r)
	}
	if got := linkedHalves(rt, c.Name, protectA1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("warrior halves %v, want the source", got)
	}
	if got := linkedHalves(rt, ally.Name, protectA1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("ally halves %v, want the recipient", got)
	}
	if _, ok := findFrame(r.Frames, wire.OpSourceEffect); !ok {
		t.Fatal("no B5ED for the warrior")
	}
	if _, ok := findFrame(r.Broadcast, wire.OpAttachedEffect); !ok {
		t.Fatal("no public B419 for the protected member")
	}

	now := clock.NowMs()
	rt.commitSkillHostility(testDivision, member, mob.Gid, enterworld.SkillRow{}, []simulation.MonsterDamageResult{{Applied: protectHit}}, now)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP != mob.CurrentHP {
		t.Fatal("the threat transfer touched HP")
	}
	if len(after.Opponents) != 2 || after.Opponents[0].GID != warrior || after.Opponents[0].Damage != 0 ||
		after.Opponents[0].Aggression != protectA1Percent {
		t.Fatalf("warrior share: %+v", after.Opponents)
	}
	if after.Opponents[1].GID != member || after.Opponents[1].Damage != protectHit ||
		after.Opponents[1].Aggression != protectHit-protectA1Percent {
		t.Fatalf("member share: %+v", after.Opponents)
	}

	key := simulation.WorldKey(testDivision, ally.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(ally) }, func(w *simulation.WorldState) { w.Spawn.X += 1600 })
	rt.advanceLinkedEffects(now)
	rt.drainStoppedCharacterEffects()
	if len(linkedHalves(rt, c.Name, protectA1))+len(linkedHalves(rt, ally.Name, protectA1)) != 0 {
		t.Fatal("the link outlived its range")
	}
	rt.commitSkillHostility(testDivision, member, mob.Gid, enterworld.SkillRow{}, []simulation.MonsterDamageResult{{Applied: protectHit}}, now)
	after, _ = rt.Monsters.Get(testDivision, mob.Gid)
	if after.Opponents[0].Aggression != protectA1Percent || after.Opponents[1].Aggression != 2*protectHit-protectA1Percent {
		t.Fatalf("retired link still diverted aggression: %+v", after.Opponents)
	}
}
