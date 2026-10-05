/*
===========================================================================

bardaura_cut_test.go - a hit on the Bard ends its aura unless skc's keep holds

CSkillManager_ProcessDamageEffects (5A160A..5A1691): skc word 2 is the keep
chance; the caster's getv MUCR (+0x548) and getv DSER (+0x54C) modifiers
add to it, and a masked hit ends the effect with 100 - keep. DSCR (+0x550)
has no reader. The rolls come from the injected CombatRoll: the first roll
on an actor's stream ends the effect when roll % 101 <= 100 - keep.

===========================================================================
*/

package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

const (
	prismTopID       = 9660 // SKILL_EU_BARD_MUSICP_REINFORCE_A_05, setv MUCR 14
	screenDanceTopID = 9929 // SKILL_EU_BARD_DANACEP_DEFENSE_A_04, setv DSCR 14
	danceRangeID     = 9919 // SKILL_EU_BARD_DANACEP_RANGE_A_01, setv DSER 50
	// bardHitFlags is an att word 0 that skc word 0 (15) matches.
	bardHitFlags = 1
)

// hitBard lands one masked hit on c with the next roll fixed at roll.
func hitBard(rt *Runtime, clock *fakeClock, c *enterworld.Character, roll uint32) {
	rt.CombatRoll = func() (uint32, error) { return roll, nil }
	rt.deps.Update(c, "test-hit", func() bool {
		rt.cancelEffectsOnDamage(testDivision, c, bardHitFlags, clock.NowMs())
		return true
	})
}

/*
================
TestBardMusicKeepChance

Guard Tambour, skc(15,0,80), on a hit to its Bard: keep 80, so a roll of 20
ends the aura (for the party too) and 21 keeps it. Prism's MUCR 14 raises
the keep to 94: 6 still ends it, 7 no longer does.
================
*/
func TestBardMusicKeepChance(t *testing.T) {
	for _, tc := range []struct {
		prism bool
		roll  uint32
		cut   bool
	}{
		{false, 20, true},
		{false, 21, false},
		{true, 6, true},
		{true, 7, false},
	} {
		t.Run(fmt.Sprintf("prism=%v/roll=%d", tc.prism, tc.roll), func(t *testing.T) {
			rt, clock, c, _ := marchFixture(t, guardTambourID)
			if tc.prism {
				learnShipped(t, rt, c, prismTopID)
			}
			mate := partyMate(rt, c, 12, "cut-mate", 100)
			setParty(rt, c, mate)
			mustCast(t, rt, clock, c, guardTambourID)
			bardTick(rt, clock, time.Millisecond)

			hitBard(rt, clock, c, tc.roll)
			bardTick(rt, clock, time.Millisecond)
			bardTick(rt, clock, time.Millisecond)
			for _, who := range []*enterworld.Character{c, mate} {
				if kept := hasSkillEffect(rt, who.Name, guardTambourID); kept == tc.cut {
					t.Errorf("%s kept the aura = %v, want %v", who.Name, kept, !tc.cut)
				}
			}
		})
	}
}

/*
================
TestBardDanceKeepChance

A dance reads getv DSER, never MUCR or DSCR. Screen Dance (setv DSCR) and
Prism (setv MUCR) leave Dancing of Valor at keep 80, so a roll of 20 ends
it. The Dancing Range passive's DSER 50 is the dance's area addend and, as
5A162E reads the same key, a keep addend too: keep 130 is held at 100 and
no hit ends the dance.
================
*/
func TestBardDanceKeepChance(t *testing.T) {
	for _, tc := range []struct {
		passive uint32
		cut     bool
	}{
		{screenDanceTopID, true},
		{prismTopID, true},
		{danceRangeID, false},
	} {
		t.Run(fmt.Sprint(tc.passive), func(t *testing.T) {
			rt, clock, c, _ := marchFixture(t, guardTambourID)
			b := rivalBard(t, rt, c, danceOfValorID)
			learnShipped(t, rt, b, tc.passive)
			mustCast(t, rt, clock, c, guardTambourID)
			bardTick(rt, clock, time.Millisecond)
			mustCast(t, rt, clock, b, danceOfValorID)

			hitBard(rt, clock, b, 0)
			bardTick(rt, clock, time.Millisecond)
			bardTick(rt, clock, time.Millisecond)
			if kept := hasSkillEffect(rt, b.Name, danceOfValorID); kept == tc.cut {
				t.Fatalf("dance kept = %v, want %v", kept, !tc.cut)
			}
		})
	}
}

/*
================
TestBardAuraIgnoresHitsOnMembers

The rule cuts the aura when its Bard is hit. A hit on a member, even with
a roll that would always cut, leaves the member's copy alone.
================
*/
func TestBardAuraIgnoresHitsOnMembers(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "hit-member", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)

	hitBard(rt, clock, mate, 0)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("a hit on a member cut its copy of the aura")
	}
}
