/*
===========================================================================

linkedblessing_test.go - the Cleric's Force and Mental Blessings

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	forceBlessingA1  = 10380 // SKILL_EU_CLERIC_BLESSA_STR_A_01: lnks 5 1500 4 1, stri 4 50, getv HLFS
	mentalBlessingA1 = 10398 // SKILL_EU_CLERIC_BLESSA_INT_A_01: lnks 6 1500 4 1, inti 9 50, getv HLMI
)

func linkedHalves(rt *Runtime, name string, id uint32) (phases []uint8) {
	for _, e := range rt.effects.Snapshot(testDivision, name) {
		if e.SkillID == id && e.LinkToken != 0 && !e.StopRequested {
			phases = append(phases, e.Phase)
		}
	}
	return phases
}

/*
==================
TestForceBlessingLinksCasterAndSubject

The caster keeps the source half (mode 1, private B5ED naming the subject)
and the ally the recipient half (mode 2, public B419) carrying stri: 4
plus the caster's HLFS, capped at 50 % of the ally's current strength.
The caster itself is not a subject, a second cast on the same ally is
300c, and leaving the 1500 link range retires both halves and the bonus.
==================
*/
func TestForceBlessingLinksCasterAndSubject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hlfs  uint32
		bonus float32
	}{{"authored", 0, 4}, {"capped", 40, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c := concealmentFixture(t, forceBlessingA1)
			if tc.hlfs != 0 {
				var charity enterworld.SkillRow
				charity.ID, charity.Group, charity.Level = 90003, 90003, 1
				charity.PassiveParameters.Pinned = true
				charity.PassiveParameters.Mask = enterworld.SkillParameterMask(1) << enterworld.ParameterBlessStrength
				charity.PassiveParameters.Values[enterworld.ParameterBlessStrength] = tc.hlfs
				rt.deps.SkillData().(staticSkillSource)[charity.ID] = charity
				c.Skills = append(c.Skills, charity.ID)
			}
			ally := nearbyCharacter(rt, c, 21, "ally", 1)
			before, _, err := rt.playerCombatStats(testDivision, ally)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.bonus
			if tc.hlfs != 0 {
				want = float32(int32(before.Strength)) * 0.5 // 4 + 40 exceeds the cap
			}

			if r := castSelf(rt, c, forceBlessingA1); len(linkedHalves(rt, c.Name, forceBlessingA1)) != 0 || r.DiagnosticRefusal == "" && (len(r.Frames) == 0 || r.Frames[0].Payload[0] == 1) {
				t.Fatalf("the caster blessed itself: %+v", r)
			}
			cast := wire.SkillAction{ActionId: forceBlessingA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode()
			r := rt.HandleTargetInteract(testDivision, c, cast)
			if r.DiagnosticRefusal != "" {
				t.Fatalf("cast refused: %+v", r)
			}
			if got := linkedHalves(rt, c.Name, forceBlessingA1); len(got) != 1 || got[0] != 1 {
				t.Fatalf("caster halves %v, want the source", got)
			}
			if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 1 || got[0] != 2 {
				t.Fatalf("ally halves %v, want the recipient", got)
			}
			if _, ok := findFrame(r.Frames, wire.OpSourceEffect); !ok {
				t.Fatal("no B5ED for the caster")
			}
			if _, ok := findFrame(r.Broadcast, wire.OpSourceEffect); ok {
				t.Fatal("B5ED broadcast to peers")
			}
			if _, ok := findFrame(r.Broadcast, wire.OpAttachedEffect); !ok {
				t.Fatal("no public B419 for the recipient")
			}
			blessed, _, _ := rt.playerCombatStats(testDivision, ally)
			if float32(blessed.Strength-before.Strength) != want {
				t.Fatalf("ally strength %v -> %v, want +%v", before.Strength, blessed.Strength, want)
			}
			if caster, _, _ := rt.playerCombatStats(testDivision, c); caster.Strength != before.Strength {
				t.Fatalf("the source half raised the caster: %v", caster.Strength)
			}

			clock.Advance(2 * time.Second)
			rt.drainSkillFinalizes(clock.NowMs())
			if again := rt.HandleTargetInteract(testDivision, c, cast); again.DiagnosticRefusal != "" || len(again.Frames) != 1 || again.Frames[0].Payload[0] == 1 {
				t.Fatalf("second blessing on the same ally: %+v", again)
			}

			key := simulation.WorldKey(testDivision, ally.Name)
			rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(ally) }, func(w *simulation.WorldState) { w.Spawn.X += 1600 })
			rt.advanceLinkedEffects(clock.NowMs())
			rt.drainStoppedCharacterEffects()
			if len(linkedHalves(rt, c.Name, forceBlessingA1))+len(linkedHalves(rt, ally.Name, forceBlessingA1)) != 0 {
				t.Fatal("the link outlived its range")
			}
			if after, _, _ := rt.playerCombatStats(testDivision, ally); after.Strength != before.Strength {
				t.Fatalf("strength %v after the link broke, want %v", after.Strength, before.Strength)
			}
		})
	}
}

/*
==================
TestMentalBlessingRaisesIntellect

inti writes parameter 2; one caster may hold a Force and a Mental Blessing
on the same ally, since their link groups (5, 6) differ.
==================
*/
func TestMentalBlessingRaisesIntellect(t *testing.T) {
	rt, clock, c := concealmentFixture(t, forceBlessingA1, mentalBlessingA1)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	before, _, _ := rt.playerCombatStats(testDivision, ally)
	for _, id := range []uint32{forceBlessingA1, mentalBlessingA1} {
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: id, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode())
		if r.DiagnosticRefusal != "" || len(linkedHalves(rt, ally.Name, id)) != 1 {
			t.Fatalf("blessing %d: %+v", id, r)
		}
		clock.Advance(2 * time.Second)
		rt.drainSkillFinalizes(clock.NowMs())
	}
	after, _, _ := rt.playerCombatStats(testDivision, ally)
	wantInt := min(float32(9), float32(int32(before.Intellect))*0.5)
	if float32(after.Intellect-before.Intellect) != wantInt || after.Strength-before.Strength != 4 {
		t.Fatalf("int %v -> %v, str %v -> %v", before.Intellect, after.Intellect, before.Strength, after.Strength)
	}
}

/*
==================
TestBlessingRunsItsTimerAfterCasterDisconnect

#2569: the caster disconnects after blessing an ally. Native buff
entries belong to the recipient and the logout cleanup cancels only
the departing character's own list,
so the ally keeps the blessing and its stri write until the recipient
half's own timer runs out - including in the spawn row a reconnected
caster is shown (the EntrySkills this test reads feed BuildPeerSpawnRow).
==================
*/
func TestBlessingRunsItsTimerAfterCasterDisconnect(t *testing.T) {
	rt, clock, c := concealmentFixture(t, forceBlessingA1)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	before, _, err := rt.playerCombatStats(testDivision, ally)
	if err != nil {
		t.Fatal(err)
	}
	cast := wire.SkillAction{ActionId: forceBlessingA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode()
	if r := rt.HandleTargetInteract(testDivision, c, cast); r.DiagnosticRefusal != "" {
		t.Fatalf("cast refused: %+v", r)
	}
	if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("ally halves %v, want the recipient", got)
	}

	rt.ForgetCharacter(testDivision, c.Name)
	for i := 0; i < 5; i++ {
		clock.Advance(2 * time.Second)
		rt.advanceLinkedEffects(clock.NowMs())
		rt.effects.Expire(clock.NowMs())
		rt.drainStoppedCharacterEffects()
	}
	if got := linkedHalves(rt, c.Name, forceBlessingA1); len(got) != 0 {
		t.Fatalf("departed caster kept halves %v", got)
	}
	if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("ally halves after the disconnect %v, want the recipient", got)
	}
	if blessed, _, _ := rt.playerCombatStats(testDivision, ally); blessed.Strength-before.Strength != 4 {
		t.Fatalf("ally strength bonus %v after the disconnect, want 4", blessed.Strength-before.Strength)
	}
	// The reconnecting caster's view of the ally: the spawn row still
	// carries the blessing with time left.
	shown := false
	for _, s := range rt.EntrySkills(testDivision, ally.Name) {
		if s.ID == forceBlessingA1 && s.Token != nil && s.Remaining != nil && *s.Remaining > 0 {
			shown = true
		}
	}
	if !shown {
		t.Fatalf("ally spawn rows after the disconnect: %+v", rt.EntrySkills(testDivision, ally.Name))
	}

	// The recipient half ends by its own timer, not before.
	var expiresAtMs int64
	for _, e := range rt.effects.Snapshot(testDivision, ally.Name) {
		if e.SkillID == forceBlessingA1 {
			expiresAtMs = e.ExpiresAtMs
		}
	}
	clock.Advance(time.Duration(expiresAtMs-clock.NowMs()) * time.Millisecond)
	rt.effects.Expire(clock.NowMs())
	rt.drainStoppedCharacterEffects()
	if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 1 {
		t.Fatalf("ally halves at the timer equality %v, want them kept", got)
	}
	clock.Advance(time.Millisecond)
	rt.effects.Expire(clock.NowMs())
	rt.drainStoppedCharacterEffects()
	if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 0 {
		t.Fatalf("ally halves past the timer %v", got)
	}
	if after, _, _ := rt.playerCombatStats(testDivision, ally); after.Strength != before.Strength {
		t.Fatalf("ally strength %v after the timer, want %v", after.Strength, before.Strength)
	}
}

/*
==================
TestBlessingRecastAfterReconnectReplaces

#2569: the caster returns while its blessing still runs on the ally and
casts again. The fresh cast replaces the orphaned half - one live
recipient half, one stri write - instead of stacking a second copy.
==================
*/
func TestBlessingRecastAfterReconnectReplaces(t *testing.T) {
	rt, clock, c := concealmentFixture(t, forceBlessingA1)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	before, _, err := rt.playerCombatStats(testDivision, ally)
	if err != nil {
		t.Fatal(err)
	}
	cast := wire.SkillAction{ActionId: forceBlessingA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode()
	if r := rt.HandleTargetInteract(testDivision, c, cast); r.DiagnosticRefusal != "" {
		t.Fatalf("cast refused: %+v", r)
	}
	rt.ForgetCharacter(testDivision, c.Name)
	clock.Advance(2 * time.Second)
	rt.advanceLinkedEffects(clock.NowMs())
	rt.drainStoppedCharacterEffects()

	if r := rt.HandleTargetInteract(testDivision, c, cast); r.DiagnosticRefusal != "" {
		t.Fatalf("recast refused: %+v", r)
	}
	clock.Advance(2 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	rt.drainStoppedCharacterEffects()
	if got := linkedHalves(rt, ally.Name, forceBlessingA1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("ally halves after the recast %v, want one recipient", got)
	}
	if got := linkedHalves(rt, c.Name, forceBlessingA1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("caster halves after the recast %v, want the source", got)
	}
	if blessed, _, _ := rt.playerCombatStats(testDivision, ally); blessed.Strength-before.Strength != 4 {
		t.Fatalf("ally strength bonus %v after the recast, want 4 (not stacked)", blessed.Strength-before.Strength)
	}
}
