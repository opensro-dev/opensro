/*
===========================================================================

bardaura_dance_test.go - the Dancings and what they hand out

Owner's rule 4 of the Bard specification: a Dancing needs the dancer inside
another Bard's music (reqc 32) and is a party aura with an MP pulse.
Dancing of Healing and Vitality raise the healing their party receives
(rhru); Dancing of Mana cuts its MP consumption (dcmp).

===========================================================================
*/

package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

const (
	danceOfHealingID  = 9953 // SKILL_EU_BARD_DANCEA_CLERIC_A_01, rhru(18,0)
	danceOfVitalityID = 9958 // SKILL_EU_BARD_DANCEA_CLERIC_B_01, rhru(38,0)
	danceOfManaID     = 9962 // SKILL_EU_BARD_DANCEA_ROG_A_01, dcmp(22)
)

/*
================
dancingParty

Bard c plays Guard Tambour; the second Bard, in its music, dances id.
================
*/
func dancingParty(t *testing.T, id uint32) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	b := rivalBard(t, rt, c, id)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	mustCast(t, rt, clock, b, id)
	bardTick(rt, clock, time.Millisecond)
	return rt, clock, c, b
}

/*
================
TestDancesOfHealingVitalityAndManaArePartyAuras

The three Dancings refused as unsupported shapes are party auras: the
dancer holds the instance, the rest of the party joins, and the dancer
pays the onff pulse.
================
*/
func TestDancesOfHealingVitalityAndManaArePartyAuras(t *testing.T) {
	for _, id := range []uint32{danceOfHealingID, danceOfVitalityID, danceOfManaID} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			rt, clock, c, b := dancingParty(t, id)
			if !hasSkillEffect(rt, c.Name, id) {
				t.Fatal("the other Bard did not join the dance")
			}
			before := *b.CurrentMP
			bardTick(rt, clock, auraPulse)
			if *b.CurrentMP >= before {
				t.Fatalf("the dancer paid no pulse: MP %d, then %d", before, *b.CurrentMP)
			}
		})
	}
}

/*
================
TestDanceNeedsAnotherBardsMusic

A Bard alone, playing its own Guard Tambour, cannot dance; inside another
Bard's Guard Tambour it can.
================
*/
func TestDanceNeedsAnotherBardsMusic(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	learnAura(t, rt, c, danceOfValorID)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, bardCastGap)
	if result := castSelf(rt, c, danceOfValorID); hasSkillEffect(rt, c.Name, danceOfValorID) {
		t.Fatalf("a Bard danced to its own music: %+v", result)
	}

	b := rivalBard(t, rt, c, guardTambourID)
	mustCast(t, rt, clock, b, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	mustCast(t, rt, clock, c, danceOfValorID)
}

/*
================
TestDancingOfHealingRaisesTheHealReceived

A heal on a member of the Dancing of Healing (rhru 18) and of the Dancing
of Vitality (rhru 38) is raised by that percent; the heal of a member
outside any dance is not.
================
*/
func TestDancingOfHealingRaisesTheHealReceived(t *testing.T) {
	heal := enterworld.SkillRow{Heal: enterworld.SkillHeal{Present: true, HP: 1000, MP: 1000}}
	for _, tc := range []struct {
		id uint32
		up float32
	}{{danceOfHealingID, 18}, {danceOfVitalityID, 38}} {
		t.Run(fmt.Sprint(tc.id), func(t *testing.T) {
			rt, _, c, b := marchFixtureWithRival(t)
			plainHP, plainMP, ok := rt.skillHealAmounts(testDivision, c, b, heal, healCast)
			if !ok {
				t.Fatal("heal amounts unavailable")
			}
			rt, _, c, b = dancingParty(t, tc.id)
			hp, mp, ok := rt.skillHealAmounts(testDivision, c, b, heal, healCast)
			if !ok {
				t.Fatal("heal amounts unavailable")
			}
			if hp != healScale(plainHP, tc.up) || mp != plainMP {
				t.Fatalf("heal received %d HP / %d MP, want %d / %d", hp, mp, healScale(plainHP, tc.up), plainMP)
			}
		})
	}
}

/*
================
TestDancingOfManaLowersTheMPCost

A member of the Dancing of Mana (dcmp 22) pays 22 % less MP for a cast,
cut the way MP Decrease cuts it.
================
*/
func TestDancingOfManaLowersTheMPCost(t *testing.T) {
	rt, _, c, _ := marchFixtureWithRival(t)
	row := rt.deps.SkillData().(staticSkillSource)[guardTambourID]
	plain, err := rt.preparedExecutionMPCost(testDivision, c, row)
	if err != nil || plain == 0 {
		t.Fatalf("plain cost %d, %v", plain, err)
	}
	rt, _, c, _ = dancingParty(t, danceOfManaID)
	cost, err := rt.preparedExecutionMPCost(testDivision, c, row)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(combat.CutMPCost(int32(plain), 22)); cost != want || cost >= plain {
		t.Fatalf("cost under Dancing of Mana %d, want %d (plain %d)", cost, want, plain)
	}
}

// marchFixtureWithRival is the dancing party's two Bards before any cast.
func marchFixtureWithRival(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	return rt, clock, c, rivalBard(t, rt, c)
}
