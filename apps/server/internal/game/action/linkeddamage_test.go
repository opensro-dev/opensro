/*
===========================================================================

linkeddamage_test.go - fence and Pain Quota shares of a member's hit

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
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
member keeps 77 and the warrior takes 23 as a deferred damage tick.
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
	shared := rt.strikeLinkedShares(testDivision, 9001, deathKiller{}, enterworld.SkillRow{ID: 1}, out.linkMoves, now, rt.tickDoors())
	if got := before - *warrior.CurrentHP; got != 23 {
		t.Fatalf("warrior lost %d, want 23", got)
	}
	// A share reaches the taker as a deferred damage tick (the pulse
	// packet's tick mode), never as a cast result: observers show the
	// tick number and take the taker's HP from the vitals refresh.
	if len(shared.Broadcast) == 0 || shared.Broadcast[0].Opcode != wire.OpSkillPulse {
		t.Fatalf("share not published as a tick: %+v", shared.Broadcast)
	}
	tick := shared.Broadcast[0].Payload
	warriorGID := enterworld.ObjectIDForCharacter(warrior)
	wantTick := []byte{1, 0x29, 0x23, 0x00, 0x00, 0,
		byte(warriorGID), byte(warriorGID >> 8), byte(warriorGID >> 16), byte(warriorGID >> 24),
		1, 23, 0, 0}
	if !bytes.Equal(tick, wantTick) {
		t.Fatalf("tick payload %v, want %v", tick, wantTick)
	}
	vitals := false
	for _, frame := range shared.Broadcast {
		if frame.Opcode == simulation.OpVitalsUpdate {
			vitals = true
		}
	}
	if !vitals {
		t.Fatalf("share carries no vitals refresh: %+v", shared.Broadcast)
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
TestFenceKillingShareSetsTheTicksDeathByte

A share larger than the warrior's remaining HP sets the tick's death
byte, and the dead-state frame that owns the death rides the same
broadcast after the tick, so observers commit the number and the
death in one batch.
================
*/
func TestFenceKillingShareSetsTheTicksDeathByte(t *testing.T) {
	rt, c, warrior, now := linkedShareFixture(t, 33, 0)
	*warrior.CurrentHP = 10
	var out playerStruck
	rt.shareLinkedDamageInDoor(playerStrike{division: testDivision, victim: c, now: now},
		combat.Result{Damage: 100, PhysicalDamage: 70, MagicalDamage: 30, ResultFlags: 1}, &out)
	if len(out.linkMoves) != 1 {
		t.Fatalf("moves %+v", out.linkMoves)
	}
	shared := rt.strikeLinkedShares(testDivision, 9001, deathKiller{}, enterworld.SkillRow{ID: 1}, out.linkMoves, now, rt.tickDoors())
	if enterworld.CharacterAlive(warrior) {
		t.Fatal("warrior survived a killing share")
	}
	if len(shared.Broadcast) == 0 || shared.Broadcast[0].Opcode != wire.OpSkillPulse {
		t.Fatalf("share not published as a tick: %+v", shared.Broadcast)
	}
	tick := shared.Broadcast[0].Payload
	if len(tick) < 6 || tick[0] != 1 || tick[5] != 0x80 {
		t.Fatalf("killing tick %v, want mode 1 with the death byte set", tick)
	}
	dead := false
	for _, frame := range shared.Broadcast[1:] {
		if frame.Opcode == wire.OpObjectStateRefresh {
			dead = true
		}
	}
	if !dead {
		t.Fatalf("killing tick broadcast carries no dead state: %+v", shared.Broadcast)
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
