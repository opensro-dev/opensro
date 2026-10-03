/*
===========================================================================

bardaura_cut_test.go - a hit on the Bard cuts its aura with skc's chance

Owner's rules 1 and 4 of the Bard specification: skc(15,0,80) ends the
aura on 80 % of the hits its Bard receives, Prism (setv MUCR) takes its
points off that chance for the music auras and Screen Dance (setv DSCR)
for the dances. The rolls come from the injected CombatRoll: the first
roll on an actor's stream cuts when roll % 101 <= chance.

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
TestBardMusicCutChance

Guard Tambour on a hit to its Bard: a roll inside the 80 % cuts the aura,
for the party too; above it the aura stays. Prism's 14 points leave 66 %:
66 still cuts, 67 no longer does.
================
*/
func TestBardMusicCutChance(t *testing.T) {
	for _, tc := range []struct {
		prism bool
		roll  uint32
		cut   bool
	}{
		{false, 80, true},
		{false, 81, false},
		{true, 66, true},
		{true, 67, false},
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
TestBardDanceCutChance

A dance reads Screen Dance, not Prism: with Screen Dance a roll of 67 keeps
Dancing of Valor, with Prism alone the same roll still cuts it.
================
*/
func TestBardDanceCutChance(t *testing.T) {
	for _, tc := range []struct {
		passive uint32
		cut     bool
	}{
		{screenDanceTopID, false},
		{prismTopID, true},
	} {
		t.Run(fmt.Sprint(tc.passive), func(t *testing.T) {
			rt, clock, c, _ := marchFixture(t, guardTambourID)
			b := rivalBard(t, rt, c, danceOfValorID)
			learnShipped(t, rt, b, tc.passive)
			mustCast(t, rt, clock, c, guardTambourID)
			bardTick(rt, clock, time.Millisecond)
			mustCast(t, rt, clock, b, danceOfValorID)

			hitBard(rt, clock, b, 67)
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
