/*
===========================================================================

bardaura_rivals_test.go - two Bards of one party playing instruments

Owner's rule 3 of the Bard specification: the lower-level instrument aura
is cancelled and the higher one stays, unless a Dancing plays in the party
and the two instruments are different.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const danceOfValorID = 9932 // SKILL_EU_BARD_DANCEA_WARRIOR_A_01

/*
================
rivalBard

A second Bard in c's party, 100 units away, with c's harp and its own MP,
knowing ids.
================
*/
func rivalBard(t *testing.T, rt *Runtime, c *enterworld.Character, ids ...uint32) *enterworld.Character {
	t.Helper()
	b := partyMate(rt, c, 12, "rival-bard", 100)
	for _, id := range ids {
		learnAura(t, rt, b, id)
	}
	setParty(rt, c, b)
	return b
}

/*
================
TestRivalBardsKeepTheHigherInstrument

Without a dance the lower-level tambour ends, whichever Bard cast first:
the required mastery level orders them, not the cast order.
================
*/
func TestRivalBardsKeepTheHigherInstrument(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second uint32
		survivor      uint32
	}{
		{"newer-higher", guardTambourID, manaTambourID, manaTambourID},
		{"older-higher", guardTambour8ID, manaTambourID, guardTambour8ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, _ := marchFixture(t, tc.first)
			b := rivalBard(t, rt, c, tc.second)

			mustCast(t, rt, clock, c, tc.first)
			mustCast(t, rt, clock, b, tc.second)
			bardTick(rt, clock, time.Millisecond)
			bardTick(rt, clock, time.Millisecond)

			for _, id := range []uint32{tc.first, tc.second} {
				for _, who := range []*enterworld.Character{c, b} {
					if got, want := hasSkillEffect(rt, who.Name, id), id == tc.survivor; got != want {
						t.Errorf("%s holds %d = %v, want %v", who.Name, id, got, want)
					}
				}
			}
		})
	}
}

/*
================
TestRivalBardsShareDifferentInstrumentsWhileADanceIsActive

The second Bard dances under the first Bard's Guard Tambour, then plays
Mana Tambour: both tambours stay while the dance lasts. When the dance
stops the lower one, Guard Tambour, ends at the next update.
================
*/
func TestRivalBardsShareDifferentInstrumentsWhileADanceIsActive(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	b := rivalBard(t, rt, c, danceOfValorID, manaTambourID)

	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	mustCast(t, rt, clock, b, danceOfValorID)
	mustCast(t, rt, clock, b, manaTambourID)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	for _, id := range []uint32{guardTambourID, manaTambourID} {
		if !hasSkillEffect(rt, c.Name, id) || !hasSkillEffect(rt, b.Name, id) {
			t.Fatalf("tambour %d did not stay beside the other while the dance plays", id)
		}
	}

	var dance uint32
	for _, e := range rt.effects.Snapshot(testDivision, b.Name) {
		if e.SkillID == danceOfValorID {
			dance = e.InstanceToken
		}
	}
	rt.HandleTargetInteract(testDivision, b, wire.CancelActiveEffectRequest{EffectID: danceOfValorID, InstanceToken: dance}.Encode())
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, c.Name, guardTambourID) || hasSkillEffect(rt, b.Name, guardTambourID) {
		t.Fatal("the lower tambour stayed after the dance stopped")
	}
	if !hasSkillEffect(rt, c.Name, manaTambourID) {
		t.Fatal("the higher tambour ended")
	}
}

/*
================
TestRivalBardsNeverShareOneInstrumentKind

A dance lets different instruments coexist, never two of one kind: the
lower Guard Tambour ends even while the dance plays.
================
*/
func TestRivalBardsNeverShareOneInstrumentKind(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	b := rivalBard(t, rt, c, danceOfValorID, guardTambour8ID)

	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	mustCast(t, rt, clock, b, danceOfValorID)
	mustCast(t, rt, clock, b, guardTambour8ID)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, c.Name, guardTambourID) || !hasSkillEffect(rt, c.Name, guardTambour8ID) {
		t.Fatal("two Guard Tambours shared the party")
	}
}
