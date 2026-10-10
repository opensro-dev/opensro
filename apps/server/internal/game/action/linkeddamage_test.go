/*
===========================================================================

linkeddamage_test.go - fence and Pain Quota shares of a member's hit

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
linkedShareFixture

c holds a link from warrior (source) with the given fence or quota.
================
*/
func linkedShareFixture(t *testing.T, fence, quota uint32) (*Runtime, *enterworld.Character, *enterworld.Character, int64) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 10000)
	warrior := nearbyCharacter(rt, c, 20, "warrior", 30)
	// Its own HP: the copy would otherwise share c's.
	hp := *c.CurrentHP
	warrior.CurrentHP = &hp
	l := statuseffect.Link{DivisionID: testDivision, SourceName: warrior.Name, TargetName: c.Name,
		SourceGID: enterworld.ObjectIDForCharacter(warrior), TargetGID: enterworld.ObjectIDForCharacter(c),
		SourceToken: 100, TargetToken: 101, SkillID: 7217, SkillGroup: 400, Group: 1, MaxOutgoing: 2, MaxDistance: 1500,
		FenceMask: 4 | 1 | 2, FencePercent: fence, QuotaPercent: quota, ExpiresAtMs: clock.NowMs() + 60000}
	if code := rt.effects.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}
	return rt, c, warrior, clock.NowMs()
}

/*
================
TestFenceMovesThePhysicalShareToTheWarrior

A Physical Fence of 33 takes 23 of a 70 physical / 30 magical hit; the
member keeps 77 and the warrior takes 23 as the attacker's pulse hit.
================
*/
func TestFenceMovesThePhysicalShareToTheWarrior(t *testing.T) {
	rt, c, warrior, now := linkedShareFixture(t, 33, 0)
	var out playerStruck
	kept := rt.shareLinkedDamageInDoor(playerStrike{division: testDivision, victim: c, now: now},
		combat.Result{Damage: 100, PhysicalDamage: 70, MagicalDamage: 30, ResultFlags: 1}, &out)
	if kept.Damage != 77 || len(out.linkMoves) != 1 || out.linkMoves[0] != (linkedMove{gid: enterworld.ObjectIDForCharacter(warrior), amount: 23}) {
		t.Fatalf("kept %+v moves %+v", kept, out.linkMoves)
	}
	before := *warrior.CurrentHP
	shared := rt.strikeLinkedShares(testDivision, 9001, deathKiller{}, enterworld.SkillRow{ID: 1}, out.linkMoves, now)
	if got := before - *warrior.CurrentHP; got != 23 {
		t.Fatalf("warrior lost %d, want 23", got)
	}
	if len(shared.Broadcast) == 0 || shared.Broadcast[0].Opcode != wire.OpSkillPulse {
		t.Fatalf("share not published as a pulse: %+v", shared.Broadcast)
	}
	// A moved share does not run the taker's own links again.
	var again playerStruck
	if hit := rt.shareLinkedDamageInDoor(playerStrike{division: testDivision, victim: warrior, now: now},
		combat.Result{Damage: 50, PhysicalDamage: 50}, &again); hit.Damage != 50 || len(again.linkMoves) != 0 {
		t.Fatal("the warrior's hit was shared without a link of its own")
	}
}

/*
================
TestPainQuotaDividesAmongPartyMembersInRange

Pain Quota 35 on c keeps 650 of 1000 and divides 350 between the one
party member within 1000; a member out of range takes nothing.
================
*/
func TestPainQuotaDividesAmongPartyMembersInRange(t *testing.T) {
	rt, c, warrior, now := linkedShareFixture(t, 0, 35)
	far := nearbyCharacter(rt, c, 21, "far-member", 2000)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(warrior), enterworld.ObjectIDForCharacter(far)}}}
	}
	members := rt.planQuotaMembers(testDivision, c, now)
	if len(members) != 1 || members[0] != enterworld.ObjectIDForCharacter(warrior) {
		t.Fatalf("members %v", members)
	}
	var out playerStruck
	kept := rt.shareLinkedDamageInDoor(playerStrike{division: testDivision, victim: c, quotaMembers: members, now: now},
		combat.Result{Damage: 1000, PhysicalDamage: 1000, ResultFlags: 1}, &out)
	if kept.Damage != 650 || len(out.linkMoves) != 1 || out.linkMoves[0].amount != 350 {
		t.Fatalf("kept %+v moves %+v", kept, out.linkMoves)
	}
	// Without a party nothing moves.
	rt.RewardParties = nil
	if members := rt.planQuotaMembers(testDivision, c, now); len(members) != 0 {
		t.Fatalf("members without a party: %v", members)
	}
}
