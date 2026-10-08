/*
===========================================================================

bardaura_test.go - the Bard's instrument and dance auras: one per Bard and
their lifetime

Owner's rules 1, 2 and 4 of the Bard specification, through the real cast
command, the simulation tick and the shipped skill rows: a Bard plays one
instrument aura at a time beside its timed Moving March, the aura ends when
the Bard runs out of MP, dies or goes through a loading screen, and a member
who lost its child, by leaving the radius or any other way, gets a new one
when it is back in range.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	guardTambourID  = 9746 // SKILL_EU_BARD_BATTLAA_GUARD_A_01, mastery 20
	guardTambour8ID = 9753 // SKILL_EU_BARD_BATTLAA_GUARD_A_08, mastery 90
	manaTambourID   = 9757 // SKILL_EU_BARD_BATTLAA_GUARD_B_01, mastery 30
	auraRadius      = 700  // efr(2,1,700,8,0,5)
	auraPulse       = 5000 * time.Millisecond
	// bardCastGap clears one cast's action bracket and the 10 s cooldown
	// the Bard's auras share before the next cast.
	bardCastGap = 11 * time.Second
	// musicLifeTestID is a stand-in Music Life passive filing BDMD at
	// musicLifeTestPercent.
	musicLifeTestID      = 900002
	musicLifeTestPercent = 50
)

/*
================
learnAura

Learn one more shipped row on c, keeping its reqi and weapon gate, the way
marchFixture learns its first.
================
*/
func learnAura(t *testing.T, rt *Runtime, c *enterworld.Character, id uint32) enterworld.SkillRow {
	t.Helper()
	row, ok := shippedSkills(t).SkillByID(id)
	if !ok {
		t.Fatalf("missing shipped skill %d", id)
	}
	rt.deps.SkillData().(staticSkillSource)[id] = row
	c.Skills = append(c.Skills, id)
	return row
}

/*
================
partyMate

A party member dx units from c with its own vitals: nearbyCharacter copies
the pointers, and a member must not share the Bard's MP or HP.
================
*/
func partyMate(rt *Runtime, c *enterworld.Character, id int64, name string, dx float64) *enterworld.Character {
	m := nearbyCharacter(rt, c, id, name, dx)
	m.CurrentHP = testInt64(100)
	m.CurrentMP = testInt64(enterworld.DerivedMaxMP(m))
	return m
}

// setParty puts every character in one party.
func setParty(rt *Runtime, members ...*enterworld.Character) {
	gids := make([]uint32, len(members))
	for i, m := range members {
		gids[i] = enterworld.ObjectIDForCharacter(m)
	}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
}

/*
================
moveCharacter

Place m dx units east of c's live position, in the world state the aura
walks read.
================
*/
func moveCharacter(rt *Runtime, m, c *enterworld.Character, dx float64) {
	to := rt.LiveSpawnFor(testDivision, c)
	to.X += dx
	rt.Worlds.Update(simulation.WorldKey(testDivision, m.Name), func() simulation.WorldState { return simulation.SeedWorldState(m) }, func(w *simulation.WorldState) {
		w.Spawn = to
		w.MoveSegment = nil
	})
}

// returnKey is the return-scroll job key of c.
func returnKey(c *enterworld.Character) string { return simulation.WorldKey(testDivision, c.Name) }

// bardTick advances the clock by d and runs one simulation tick.
func bardTick(rt *Runtime, clock *fakeClock, d time.Duration) {
	clock.Advance(d)
	rt.TickHook()(clock.NowMs())
}

// mustCast casts id on c after the previous cast's bracket closed.
func mustCast(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, id uint32) {
	t.Helper()
	bardTick(rt, clock, bardCastGap)
	if result := castSelf(rt, c, id); result.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, id) {
		t.Fatalf("%s: cast %d refused: %+v", c.Name, id, result)
	}
}

/*
================
TestBardKeepsOneInstrumentAuraBesideMovingMarch

Owner's rules 1 and 2: Mana Tambour replaces the Guard Tambour the same Bard
was playing, on the Bard and on its party, while Moving March, a timed buff,
stays through both casts.
================
*/
func TestBardKeepsOneInstrumentAuraBesideMovingMarch(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	learnAura(t, rt, c, manaTambourID)
	learnAura(t, rt, c, movingMarchFirstID)
	mate := partyMate(rt, c, 12, "one-aura-mate", 100)
	setParty(rt, c, mate)

	mustCast(t, rt, clock, c, guardTambourID)
	mustCast(t, rt, clock, c, movingMarchFirstID)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) || !hasSkillEffect(rt, mate.Name, movingMarchFirstID) {
		t.Fatal("the member did not receive the tambour and the march")
	}
	mustCast(t, rt, clock, c, manaTambourID)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)

	for _, who := range []*enterworld.Character{c, mate} {
		if hasSkillEffect(rt, who.Name, guardTambourID) {
			t.Errorf("%s still holds Guard Tambour after Mana Tambour", who.Name)
		}
		if !hasSkillEffect(rt, who.Name, manaTambourID) || !hasSkillEffect(rt, who.Name, movingMarchFirstID) {
			t.Errorf("%s lost Mana Tambour or Moving March", who.Name)
		}
	}
}

/*
================
TestBardReplacesItsInstrumentWithALowerRank

Owner's rule 1 replaces the instrument a Bard plays with whatever it casts
next, a lower rank of the same line included. The native source admission
(58E2F4) would refuse that downgrade with 0x300C; the owner's rule decides.
================
*/
func TestBardReplacesItsInstrumentWithALowerRank(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambour8ID)
	learnAura(t, rt, c, guardTambourID)
	mate := partyMate(rt, c, 12, "lower-rank-mate", 100)
	setParty(rt, c, mate)

	mustCast(t, rt, clock, c, guardTambour8ID)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)

	for _, who := range []*enterworld.Character{c, mate} {
		if hasSkillEffect(rt, who.Name, guardTambour8ID) || !hasSkillEffect(rt, who.Name, guardTambourID) {
			t.Errorf("%s does not hold the lower Guard Tambour alone", who.Name)
		}
	}
}

/*
================
TestBardAuraEndsWhenItsBardRunsOutOfMP

Owner's rule 1: the pulse that finds less MP than its onff word ends the
aura on the Bard and on the party.
================
*/
func TestBardAuraEndsWhenItsBardRunsOutOfMP(t *testing.T) {
	rt, clock, c, row := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "mp-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member did not join")
	}

	c.CurrentMP = testInt64(int64(row.Aura.PulseMP) - 1)
	bardTick(rt, clock, auraPulse)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, c.Name, guardTambourID) || hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the aura outlived its Bard's MP")
	}
}

/*
================
TestBardAuraEndsWithItsBardsDeath

Owner's rule 1: the Bard's death ends its aura for the whole party.
================
*/
func TestBardAuraEndsWithItsBardsDeath(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "death-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)

	c.CurrentHP = testInt64(0)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, c.Name, guardTambourID) || hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the aura outlived its Bard")
	}
}

/*
================
TestBardAuraEndsOnALoadingScreen

Owner's rule 1: a return to town (a loading screen, as any teleport) ends
the Bard's aura at once, and the party loses it at the next update.
================
*/
func TestBardAuraEndsOnALoadingScreen(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "loading-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member did not join")
	}

	job := pendingReturn{division: testDivision, name: c.Name, character: c, due: clock.NowMs()}
	rt.returnCasts.Store(returnKey(c), job)
	c.NativeTeleportMode = 1
	rt.completeReturnScroll(job, clock.NowMs())
	if hasSkillEffect(rt, c.Name, guardTambourID) {
		t.Fatal("the Bard kept its aura through the loading screen")
	}
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member kept the aura of a Bard who left")
	}
}

/*
================
TestBardAuraMemberLeavesAndReturns

Owner's rule 1: a member leaving the radius loses the aura and gets it
back on returning.
================
*/
func TestBardAuraMemberLeavesAndReturns(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "radius-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member did not join")
	}

	moveCharacter(rt, mate, c, auraRadius+100)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member kept the aura outside the radius")
	}
	moveCharacter(rt, mate, c, 100)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member did not get the aura back")
	}
}

/*
================
TestBardAuraIgnoresTheMembersEquipment

Live finding: an equipment move of a member without a harp ran the reqi
re-check on its child and ended the Bard's aura on it. The reqi is the
Bard's; the member's equipment must not end the child.
================
*/
func TestBardAuraIgnoresTheMembersEquipment(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "reqi-mate", 100)
	mate.MissionInventory = nil
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)

	rt.deps.Update(mate, "test-reqi", func() bool {
		rt.publishEndedEffects(testDivision, mate, rt.retireUnmetEquipmentEffects(testDivision, mate), clock.NowMs())
		return true
	})
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member's equipment re-check ended the Bard's aura on it")
	}
}

/*
================
TestBardAuraRejoinsARevivedMember

Live finding: a member whose child ended inside the radius (here its death,
which retires every transient effect) was never joined again, because the
area kept the dead token. Once revived in range it must get the aura back.
================
*/
func TestBardAuraRejoinsARevivedMember(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "revived-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)

	rt.deps.Update(mate, "test-death", func() bool {
		mate.CurrentHP = testInt64(0)
		rt.retireBodyEffectsOnDeath(testDivision, mate)
		return true
	})
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("a dead member holds the aura")
	}
	mate.CurrentHP = testInt64(100)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the revived member was not joined again")
	}
}

/*
================
TestBardAuraPulseTestsTheRawWordAndChargesTheCut

585277..585284 compares the Bard's MP with the raw onff word 1 before any
cut; the charge is the word cut by BDMD (Music Life). Holding the raw word
keeps the aura and pays only the cut; one MP less ends it even though the
cut charge would fit.
================
*/
func TestBardAuraPulseTestsTheRawWordAndChargesTheCut(t *testing.T) {
	rt, clock, c, row := marchFixture(t, guardTambourID)
	if !row.Attack.Parameters.Has(enterworld.ParameterBardMPDecrease) {
		t.Fatalf("Guard Tambour does not read BDMD: %v", row.Attack.Parameters)
	}
	passive := enterworld.SkillRow{ID: musicLifeTestID, Group: musicLifeTestID, PassiveParameters: enterworld.SkillPassiveParameters{
		Pinned: true,
		Mask:   1 << enterworld.ParameterBardMPDecrease,
		Values: enterworld.SkillParameterValues{enterworld.ParameterBardMPDecrease: musicLifeTestPercent},
	}}
	rt.deps.SkillData().(staticSkillSource)[passive.ID] = passive
	c.Skills = append(c.Skills, passive.ID)
	word := int64(row.Aura.PulseMP)
	cost := int64(combat.CutMPCost(int32(row.Aura.PulseMP), musicLifeTestPercent))
	if cost >= word {
		t.Fatalf("cut cost %d is not below the onff word %d", cost, word)
	}
	mustCast(t, rt, clock, c, guardTambourID)

	c.CurrentMP = testInt64(word)
	bardTick(rt, clock, auraPulse)
	if !hasSkillEffect(rt, c.Name, guardTambourID) || *c.CurrentMP != word-cost {
		t.Fatalf("a Bard holding the raw word lost its aura or paid wrong: MP %d, want %d", *c.CurrentMP, word-cost)
	}
	c.CurrentMP = testInt64(word - 1)
	bardTick(rt, clock, auraPulse)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, c.Name, guardTambourID) {
		t.Fatal("the aura outlived an MP below the raw onff word")
	}
}

/*
================
TestBardAuraLeavesTheExMembersOfADissolvedParty

Live finding: when the other member of a two-member party left, the party
dissolved and the ex-member kept its copy while in range, because the
leave walk read no party as "no party test". A Bard with no party keeps
its aura on itself only.
================
*/
func TestBardAuraLeavesTheExMembersOfADissolvedParty(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambourID)
	mate := partyMate(rt, c, 12, "dissolved-mate", 100)
	setParty(rt, c, mate)
	mustCast(t, rt, clock, c, guardTambourID)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the member did not join")
	}

	rt.RewardParties = func(string) []RewardParty { return nil }
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, mate.Name, guardTambourID) {
		t.Fatal("the ex-member kept the aura of a dissolved party")
	}
	if !hasSkillEffect(rt, c.Name, guardTambourID) {
		t.Fatal("the Bard lost its own aura with its party")
	}
}
