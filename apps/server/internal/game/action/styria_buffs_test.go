/*
===========================================================================

styria_buffs_test.go - Styria Clash: blessings on box openings

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
)

/*
================
styriaOpenBox

Stand the character on a box spot and click that box.
================
*/
func styriaOpenBox(t *testing.T, rt *Runtime, c *enterworld.Character, index int) {
	t.Helper()
	spot := styria.BoxSpots[index]
	styriaStandAt(rt, c, simulation.Spawn{RegionID: spot.RegionID, X: spot.X, Z: spot.Z})
	outcome, handled := rt.styriaSelect(testDivision, c, 242000+uint32(index))
	if !handled || outcome.Selected == 0 {
		t.Fatalf("%s opening box %d = %+v, %v", c.Name, index, outcome, handled)
	}
}

/*
================
styriaBlessingIDs

The blessing skill ids the character currently carries, in family order.
================
*/
func styriaBlessingIDs(rt *Runtime, c *enterworld.Character) []uint32 {
	var ids []uint32
	for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
		if styriaBlessingIndex(effect.SkillID) >= 0 {
			ids = append(ids, effect.SkillID)
		}
	}
	return ids
}

func styriaHasBlessing(rt *Runtime, c *enterworld.Character, skillID uint32) bool {
	for _, id := range styriaBlessingIDs(rt, c) {
		if id == skillID {
			return true
		}
	}
	return false
}

/*
================
TestStyriaOpeningsGrantTheBlessingRotation

Each opening earns the opener their side's next blessing, both sides
cycling on their own cursor; the five coexist, the sixth opening
refreshes the first, and the modifiers reach the combat lanes (the
damage blessing raises the skill rates, the warding one lowers the
taken factors).
================
*/
func TestStyriaOpeningsGrantTheBlessingRotation(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	var log styriaPushLog
	log.arm(rt)
	styriaStartApplied(t, rt, clock, hunter, thief)

	// The hunter works the boxes in turn.
	for index := 0; index < len(styria.BoxSpots); index++ {
		styriaOpenBox(t, rt, hunter, index)
	}
	if !styriaHasBlessing(rt, hunter, 34625) {
		t.Fatalf("after four openings the hunter carries %v, want blessings through parry",
			styriaBlessingIDs(rt, hunter))
	}
	if !log.heard("Thief", "blessing of striking") {
		t.Fatalf("the match did not hear the first blessing: %v", log.byName)
	}

	// The installed instances write the native blocks into the
	// keeper: dru's words on 0x80-0x83, odar's -30 on 0xAE-0xB1,
	// the +1000 flats on parameters 3/4, hr's 20 on parameter 11.
	writes := make(map[uint16][]float32)
	for _, w := range rt.effects.ModifierWrites(testDivision, hunter.Name) {
		writes[w.Parameter] = append(writes[w.Parameter], w.Value)
	}
	for _, param := range []uint16{0x80, 0x81, 0x82, 0x83} {
		if len(writes[param]) == 0 || writes[param][0] != 30 {
			t.Fatalf("parameter %#x writes %v, want the damage blessing's 30", param, writes[param])
		}
	}
	for _, param := range []uint16{0xae, 0xaf, 0xb0, 0xb1} {
		if len(writes[param]) == 0 || writes[param][0] != -30 {
			t.Fatalf("parameter %#x writes %v, want the warding blessing's -30", param, writes[param])
		}
	}
	for _, param := range []uint16{3, 4} {
		found := false
		for _, value := range writes[param] {
			found = found || value == 1000
		}
		if !found {
			t.Fatalf("parameter %d writes %v, want the vitality blessing's 1000", param, writes[param])
		}
	}
	if len(writes[11]) == 0 || writes[11][0] != 20 {
		t.Fatalf("parameter 11 writes %v, want the accuracy blessing's 20", writes[11])
	}

	// Expiry is the family's two minutes from the grant.
	for _, effect := range rt.effects.Snapshot(testDivision, hunter.Name) {
		if effect.SkillID == 34622 &&
			(effect.ExpiresAtMs < clock.NowMs()+100*1000 || effect.ExpiresAtMs > clock.NowMs()+styriaBlessingMs) {
			t.Fatalf("the first blessing expires at %d, granted at %d", effect.ExpiresAtMs, clock.NowMs())
		}
	}

	// Every box stands empty now; after the refill the thief's first
	// opening starts their own side's order, untouched by the hunter's.
	clock.now = clock.now.Add(61 * time.Second)
	styriaOpenBox(t, rt, thief, 0)
	if !styriaHasBlessing(rt, thief, 34622) || styriaHasBlessing(rt, thief, 34623) {
		t.Fatalf("the thief carries %v, want only their first blessing", styriaBlessingIDs(rt, thief))
	}

	// A fifth hunter opening completes the family; a sixth refreshes
	// the first blessing instead of stacking it.
	styriaOpenBox(t, rt, hunter, 1)
	styriaOpenBox(t, rt, hunter, 2)
	count := 0
	for _, id := range styriaBlessingIDs(rt, hunter) {
		if id == 34622 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the refreshed first blessing stacked to %d instances", count)
	}
}

/*
================
TestStyriaBlessingsEndWithTheSeatAndTheMatch

Death drops the seat and the blessing with it; the clock's end strips
the survivor's; a kill alone grants nothing.
================
*/
func TestStyriaBlessingsEndWithTheSeatAndTheMatch(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	match := styriaStartApplied(t, rt, clock, hunter, thief)

	styriaOpenBox(t, rt, hunter, 0)
	styriaOpenBox(t, rt, thief, 1)
	rt.styriaRecordDeath(testDivision, thief, hunter, clock.NowMs())
	if len(styriaBlessingIDs(rt, thief)) != 1 {
		t.Fatal("a kill granted the killer's victim a blessing")
	}

	*hunter.CurrentHP = 0
	rt.advanceStyriaMatches(clock.NowMs())
	if got := styriaBlessingIDs(rt, hunter); len(got) != 0 {
		t.Fatalf("the dead hunter kept blessings %v", got)
	}
	if got := styriaBlessingIDs(rt, thief); len(got) != 1 {
		t.Fatalf("the survivor's blessings changed with the death: %v", got)
	}

	clock.now = time.UnixMilli(match.endsMs)
	rt.advanceStyriaMatches(clock.NowMs())
	if got := styriaBlessingIDs(rt, thief); len(got) != 0 {
		t.Fatalf("the survivor carried blessings %v past the clock", got)
	}
}

/*
================
TestStyriaNoBlessingWithoutAMatch

A box click with no live match grants nothing, and the family table
is the recorded one (ids, order, two minutes).
================
*/
func TestStyriaNoBlessingWithoutAMatch(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, _ := newStyriaRuntime(t, hunter)
	spot := styria.BoxSpots[0]
	styriaStandAt(rt, hunter, simulation.Spawn{RegionID: spot.RegionID, X: spot.X, Z: spot.Z})
	if _, handled := rt.styriaSelect(testDivision, hunter, 242000); handled {
		t.Fatal("a box answered with no match running")
	}
	if got := styriaBlessingIDs(rt, hunter); len(got) != 0 {
		t.Fatalf("a blessing landed without a match: %v", got)
	}
	for index, want := range []uint32{34622, 34623, 34624, 34625, 34626} {
		row := styriaBlessingRow(index)
		if row.ID != want || row.Group != want || row.EffectDurationMs != uint32(styriaBlessingMs) ||
			!row.TimedEffect.Pinned {
			t.Fatalf("blessing %d row = %+v, want the recorded family row", index, row)
		}
	}
	hpmp := styriaBlessingRow(2)
	if !hpmp.TimedEffect.ItemProgram || hpmp.TimedEffect.HP.Flat != 1000 || hpmp.TimedEffect.MP.Flat != 1000 {
		t.Fatalf("the vitality row = %+v, want the +1000/+1000 item program", hpmp.TimedEffect)
	}
}
