/*
===========================================================================

concealment_test.go - hiding, detection and their early retirement

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

// Shipped rows these tests cast.
const (
	rogueStealthID     = 7929  // SKILL_EU_ROG_STEALTHA_HIDING_A_01: hide 1 3 50, STDU, STSP
	wizardInvisibleBID = 8713  // SKILL_EU_WIZARD_COLDA_INVISIBLE_B_01: party, radius 200
	frenzyDetectID     = 7177  // SKILL_EU_WARRIOR_FRENZYA_DETECT_A_01: dttp 5 3, select 26
	stealthTimeID      = 7911  // SKILL_EU_ROG_STEALTHP_TIME_A_01: STDU 12000
	stealthSpeedID     = 8283  // SKILL_EU_ROG_STEALTHP_MSPEED_A_01: STSP 10
	detectScrollSkill  = 7122  // SKILL_ETC_DETECT_01_01: cbuf dtt 7 3
	rogueDetectID      = 7939  // SKILL_EU_ROG_STEALTHA_DETECT_A_01: dtt 5 3
	concealmentLearnMP = 50000 // enough for every row
)

/*
==================
learnShipped

Installs a shipped row into the fixture's skill table and the character's
learned list. The weapon requirement is lifted: these tests are about the
program, not 58D480.
==================
*/
func learnShipped(t *testing.T, rt *Runtime, c *enterworld.Character, id uint32) enterworld.SkillRow {
	t.Helper()
	row, ok := shippedSkills(t).SkillByID(id)
	if !ok {
		t.Fatalf("missing shipped skill %d", id)
	}
	row.Reqi = enterworld.SkillReqi{}
	row.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	table, ok := rt.deps.SkillData().(staticSkillSource)
	if !ok {
		table = rt.deps.SkillData().(codenameSkills).staticSkillSource
	}
	table[id] = row
	c.Skills = append(c.Skills, id)
	mp := int64(concealmentLearnMP)
	c.CurrentMP = &mp
	return row
}

// concealmentFixture is the combat fixture out of battle (58DF20 refuses a
// hide in battle) with the given rows learned.
func concealmentFixture(t *testing.T, ids ...uint32) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	for _, id := range ids {
		learnShipped(t, rt, c, id)
	}
	return rt, clock, c
}

func castSelf(rt *Runtime, c *enterworld.Character, id uint32) OpResult {
	return rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: id}.Encode())
}

func worldSpeeds(rt *Runtime, c *enterworld.Character) (float32, float32) {
	w := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	return w.MovementSpeeds()
}

/*
==================
TestStealthHidesSlowsAndTakesPassives

The stealth cast sets body status 6 and cuts speed by the hide's 50 %;
STSP gives 10 % back (0.5 x 1.1) and STDU lengthens the effect.
==================
*/
func TestStealthHidesSlowsAndTakesPassives(t *testing.T) {
	for _, passives := range []bool{false, true} {
		ids := []uint32{rogueStealthID}
		if passives {
			ids = append(ids, stealthTimeID, stealthSpeedID)
		}
		rt, clock, c := concealmentFixture(t, ids...)
		if r := castSelf(rt, c, rogueStealthID); r.DiagnosticRefusal != "" || len(r.Frames) == 0 {
			t.Fatalf("stealth cast refused: %+v", r)
		}
		if c.NativeBodyStatus != 6 {
			t.Fatalf("body status %d, want stealth 6", c.NativeBodyStatus)
		}
		factor, duration := float32(0.5), int64(60000)
		if passives {
			factor, duration = float32(float64(float32(1.1))*0.5), 72000
		}
		walk, run := worldSpeeds(rt, c)
		if walk != float32(simulation.WalkSpeed)*factor || run != float32(simulation.RunSpeed)*factor {
			t.Fatalf("passives %v: speeds %v/%v, want factor %v", passives, walk, run, factor)
		}
		effects := rt.effects.Snapshot(testDivision, c.Name)
		if len(effects) != 1 || effects[0].ExpiresAtMs != clock.NowMs()+duration {
			t.Fatalf("passives %v: effect %+v, want a %d ms hide", passives, effects, duration)
		}
	}
}

/*
==================
TestStealthEndsOnTheNextCast

skc event 2 (InitiateSkillCast): a basic attack from stealth ends the
hide, restores speed and body status - after the strike itself counted.
==================
*/
func TestStealthEndsOnTheNextCast(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	learnShipped(t, rt, c, rogueStealthID)
	castSelf(rt, c, rogueStealthID)
	if c.NativeBodyStatus != 6 {
		t.Fatal("stealth did not install")
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("attack from stealth refused: %+v", r)
	}
	if c.NativeBodyStatus != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatalf("stealth survived the attack: body %d, effects %+v", c.NativeBodyStatus, rt.effects.Snapshot(testDivision, c.Name))
	}
	if walk, _ := worldSpeeds(rt, c); walk != float32(simulation.WalkSpeed) {
		t.Fatalf("walk speed %v after stealth ended", walk)
	}
}

/*
==================
TestDamageCancelFollowsTheMasks

5A1612: a hit whose att flags share a bit with skc word 0 ends the hide
(chance word 0: every such hit); a hit with other flags does not.
==================
*/
func TestDamageCancelFollowsTheMasks(t *testing.T) {
	for _, tc := range []struct {
		flags uint32
		ends  bool
	}{{1, false}, {4, true}, {8, true}} {
		rt, _, c := concealmentFixture(t, rogueStealthID)
		castSelf(rt, c, rogueStealthID)
		rt.deps.Update(c, "test-hit", func() bool {
			rt.cancelEffectsOnDamage(testDivision, c, tc.flags, rt.Now().UnixMilli())
			return true
		})
		if ended := c.NativeBodyStatus == 0; ended != tc.ends {
			t.Fatalf("flags %d: hide ended %v, want %v", tc.flags, ended, tc.ends)
		}
	}
}

// nearbyCharacter adds a copy of c dx units away.
func nearbyCharacter(rt *Runtime, c *enterworld.Character, id int64, name string, dx float64) *enterworld.Character {
	m := *c
	m.ID, m.Name, m.Skills = id, name, nil
	m.BattleUntilMs = 0
	world := *c.World
	spawn := *world.Spawn
	x := *spawn.X + dx
	spawn.X = &x
	world.Spawn = &spawn
	m.World = &world
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &m)
	return &m
}

func hasSkillEffect(rt *Runtime, name string, id uint32) bool {
	for _, e := range rt.effects.Snapshot(testDivision, name) {
		if e.SkillID == id {
			return true
		}
	}
	return false
}

/*
==================
TestRevealLandsAroundTheCasterButNotOnItsParty

58A020 select 26: every character within 100 of the caster gets the dttp
instance, except the caster and the caster's party; one 150 away does not.
==================
*/
func TestRevealLandsAroundTheCasterButNotOnItsParty(t *testing.T) {
	rt, clock, c := concealmentFixture(t, frenzyDetectID)
	stranger := nearbyCharacter(rt, c, 11, "stranger", 30)
	mate := nearbyCharacter(rt, c, 12, "mate", 30)
	far := nearbyCharacter(rt, c, 13, "far", 150)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
	}
	r := castSelf(rt, c, frenzyDetectID)
	if r.DiagnosticRefusal != "" {
		t.Fatal(r.DiagnosticRefusal)
	}
	clock.Advance(2 * time.Second) // the 500 ms preparation
	releasePreparedSkillForTest(t, rt, clock.NowMs())
	for name, want := range map[string]bool{c.Name: false, stranger.Name: true, mate.Name: false, far.Name: false} {
		if got := hasSkillEffect(rt, name, frenzyDetectID); got != want {
			t.Errorf("%s reveal %v, want %v", name, got, want)
		}
	}
}

/*
==================
TestPartyInvisibilityCoversCasterAndParty

Select 5: the caster and its party within 200 hide; a stranger does not.
==================
*/
func TestPartyInvisibilityCoversCasterAndParty(t *testing.T) {
	rt, _, c := concealmentFixture(t, wizardInvisibleBID)
	// 1946 MP is beyond a level-1 caster; the cost is not under test.
	row := rt.deps.SkillData().(staticSkillSource)[wizardInvisibleBID]
	row.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[wizardInvisibleBID] = row
	mate := nearbyCharacter(rt, c, 12, "mate", 50)
	stranger := nearbyCharacter(rt, c, 11, "stranger", 50)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
	}
	if r := castSelf(rt, c, wizardInvisibleBID); r.DiagnosticRefusal != "" {
		t.Fatal(r.DiagnosticRefusal)
	}
	if c.NativeBodyStatus != 7 || mate.NativeBodyStatus != 7 || stranger.NativeBodyStatus != 0 {
		t.Fatalf("bodies caster %d mate %d stranger %d, want 7 7 0", c.NativeBodyStatus, mate.NativeBodyStatus, stranger.NativeBodyStatus)
	}
}

/*
==================
TestNpcActionEndsTheHide

510262 retires the hide before the NPC request is even validated.
==================
*/
func TestNpcActionEndsTheHide(t *testing.T) {
	rt, _, c := concealmentFixture(t, rogueStealthID)
	castSelf(rt, c, rogueStealthID)
	rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(999).U32(1).Payload())
	if c.NativeBodyStatus != 0 || hasSkillEffect(rt, c.Name, rogueStealthID) {
		t.Fatal("an NPC request left the hide on")
	}
}

/*
==================
TestSightIsTheCastersOwn

A dtt row installs on the caster only, with no body status.
==================
*/
func TestSightIsTheCastersOwn(t *testing.T) {
	rt, _, c := concealmentFixture(t, rogueDetectID)
	other := nearbyCharacter(rt, c, 11, "other", 10)
	if r := castSelf(rt, c, rogueDetectID); r.DiagnosticRefusal != "" {
		t.Fatal(r.DiagnosticRefusal)
	}
	if !hasSkillEffect(rt, c.Name, rogueDetectID) || hasSkillEffect(rt, other.Name, rogueDetectID) || c.NativeBodyStatus != 0 {
		t.Fatal("dtt did not stay the caster's own")
	}
}
