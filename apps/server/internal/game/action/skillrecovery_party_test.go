/*
===========================================================================

skillrecovery_party_test.go - tests for skillrecovery.go: self-targeted
heals, party-area heals and resurrections, HP-cost self heals, the
caster's charge and the cast bracket

The shipped Cleric and Bard rows are driven through HandleTargetInteract
and the cast release, as the client drives them.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
supportParty

A cleric caster with several party mates, each standing dx along X.
================
*/
type supportParty struct {
	supportPair
	mates []*enterworld.Character
}

/*
================
newSupportParty

The pair's mate stands on the caster; every offset adds one more mate.
All of them share the caster's party.
================
*/
func newSupportParty(t *testing.T, skill enterworld.SkillRow, offsets ...float64) supportParty {
	t.Helper()
	p := supportParty{supportPair: newSupportPair(t, skill)}
	p.mates = append(p.mates, p.m)
	deps := p.rt.deps.(*enterworld.Deps)
	for i, dx := range offsets {
		m := *p.m
		m.ID, m.Name = int64(10+i), "mate"+string(rune('a'+i))
		m.CurrentHP, m.CurrentMP = testInt64(1), testInt64(0)
		world := *p.m.World
		spawn := *world.Spawn
		world.Spawn = &spawn
		m.World = &world
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(
			deps.Characters.(enterworld.StaticCharacterSource)[testDivision],
			&m,
		)
		p.place(&m, dx)
		p.mates = append(p.mates, &m)
	}

	members := []uint32{enterworld.ObjectIDForCharacter(p.c)}
	for _, m := range p.mates {
		members = append(members, enterworld.ObjectIDForCharacter(m))
	}
	p.rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: members}}
	}
	return p
}

/*
================
place
================
*/
func (p supportParty) place(m *enterworld.Character, dx float64) {
	origin := p.rt.liveSpawn(simulation.WorldKey(testDivision, p.c.Name), p.c, p.clock.NowMs())
	p.rt.Worlds.Update(
		simulation.WorldKey(testDivision, m.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(m) },
		func(w *simulation.WorldState) {
			w.Spawn.X = origin.X + dx
			w.SpawnSet = true
		},
	)
}

/*
================
castReleased

Casts with the given request, releases it, and returns every batch both
phases produced, request frames first as an actor batch.
================
*/
func (p supportPair) castReleased(t *testing.T, skill enterworld.SkillRow, cast wire.SkillAction) (OpResult, []simulation.DivisionFrames) {
	t.Helper()
	r := p.rt.HandleTargetInteract(testDivision, p.c, cast.Encode())
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] == 2 {
		t.Fatalf("%s refused %q %+v", skill.Codename, r.DiagnosticRefusal, r.Frames)
	}
	var batches []simulation.DivisionFrames
	if skill.ActionCastingTimeMs > 0 {
		batches = p.rt.advanceProjectileCasts(p.clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	}
	return r, batches
}

/*
================
affordable

The level-1 fixture holds 200 MP; a higher tier's MP cost is lowered so
the cast is paid. The cost is not what these tests are about.
================
*/
func affordable(skill *enterworld.SkillRow) {
	skill.Consumption.MP = min(skill.Consumption.MP, fixtureMPCostCeiling)
}

// fixtureMPCostCeiling stays below the level-1 fixture's 200 MP.
const fixtureMPCostCeiling = 100

/*
================
framesFor

Every frame addressed to one character alone: the immediate result's
recipients and the release's private batches.
================
*/
func framesFor(id int64, r OpResult, batches []simulation.DivisionFrames) []wire.Frame {
	var out []wire.Frame
	for _, to := range r.Recipients {
		if to.CharacterID == id {
			out = append(out, to.Frames...)
		}
	}
	for _, batch := range batches {
		if batch.OnlyCharacterID != id {
			continue
		}
		for _, f := range batch.Frames {
			out = append(out, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
		}
	}
	return out
}

/*
================
vitalsFor

The 0x33A6 frames in frames that name gid.
================
*/
func vitalsFor(gid uint32, frames []wire.Frame) int {
	n := 0
	for _, f := range frames {
		if f.Opcode == simulation.OpVitalsUpdate && len(f.Payload) >= 4 &&
			binary.LittleEndian.Uint32(f.Payload) == gid {
			n++
		}
	}
	return n
}

/*
==================
TestTargetedHealOnSelfPublishesTheCastersVitals

Healing, Healing Breath, Recovery and Holy Recovery admit Self (column
26), as does the Chinese water heal. Cast with the caster's own gid, the
heal lands on the caster and its 0x33A6 reaches the caster, as a flat self
heal's does. The mate is untouched and never sees the caster's frame.
==================
*/
func TestTargetedHealOnSelfPublishesTheCastersVitals(t *testing.T) {
	for _, code := range []string{
		"SKILL_EU_CLERIC_HEALA_TARGET_A_01",
		"SKILL_EU_CLERIC_HEALA_TARGET_B_01",
		"SKILL_EU_CLERIC_RECOVERYA_TARGET_A_01",
		"SKILL_EU_CLERIC_RECOVERYA_TARGET_B_01",
		"SKILL_CH_WATER_HEAL_A_01",
	} {
		skill := shippedOffense(t, code)
		if !skill.TargetRequired || !skill.Targets.Self || !skill.Heal.Present {
			t.Fatalf("%s targets %+v heal %+v", code, skill.Targets, skill.Heal)
		}
		affordable(&skill)
		t.Run(code, func(t *testing.T) {
			p := newSupportPair(t, skill)
			casterGID := enterworld.ObjectIDForCharacter(p.c)
			request := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: casterGID}

			r, batches := p.castReleased(t, skill, request)
			if *p.c.CurrentHP <= 40 || *p.m.CurrentHP != 1 {
				t.Fatalf("caster hp %d, mate hp %d", *p.c.CurrentHP, *p.m.CurrentHP)
			}
			toCaster := append(framesFor(p.c.ID, r, batches), r.ActorPrivate...)
			if vitalsFor(casterGID, toCaster) != 1 {
				t.Fatalf("caster vitals not published: %+v %+v", r, batches)
			}
			if vitalsFor(casterGID, framesFor(p.m.ID, r, batches)) != 0 {
				t.Fatal("the caster's private vitals reached the mate")
			}
		})
	}
}

/*
==================
TestTargetedHealWithoutTargetStaysRefused

A targeted heal sent with no target is still refused. Whether the native
server aims such a cast at the caster is not known with certainty, so it
is not retargeted; the caster keeps its HP.
==================
*/
func TestTargetedHealWithoutTargetStaysRefused(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	before := *p.c.CurrentHP

	r := p.rt.HandleTargetInteract(testDivision, p.c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if r.DiagnosticRefusal != "recovery-admission-refused" {
		t.Fatalf("no-target heal %q %+v", r.DiagnosticRefusal, r.Frames)
	}
	if *p.c.CurrentHP != before {
		t.Fatalf("caster hp %d, want %d", *p.c.CurrentHP, before)
	}
}

/*
==================
TestGroupHealingHealsTheCasterAndPartyInRange

Group Healing is efr[1,1,250,8,0,5]: select bit 0 adds the caster, and
TargetSelection_Party (58BEF0) adds every living member within 250. Three
members in range are healed by the 5A0850 amount, one at 300 and one dead
member are not. Each member gets its own 0x33A6, the caster its own.
==================
*/
func TestGroupHealingHealsTheCasterAndPartyInRange(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_GROUP_A_01")
	if !skill.Recovery.PartyHealPinned {
		t.Fatalf("group healing not admitted: %+v", skill.Recovery)
	}
	p := newSupportParty(t, skill, 100, 240, 300, 50)
	inRange, far, dead := p.mates[:3], p.mates[3], p.mates[4]
	*dead.CurrentHP = 0

	want, _, ok := p.rt.skillHealAmounts(testDivision, p.mates[0], p.c, skill, healCast)
	maxHP, _, _, _ := p.rt.playerKeeperVitals(testDivision, p.mates[0])
	if !ok || want <= 0 {
		t.Fatalf("heal amount %d %v", want, ok)
	}
	healed := min(maxHP, 1+want)

	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	for _, m := range inRange {
		if *m.CurrentHP != healed {
			t.Errorf("%s hp %d, want %d", m.Name, *m.CurrentHP, healed)
		}
		if vitalsFor(enterworld.ObjectIDForCharacter(m), framesFor(m.ID, r, batches)) != 1 {
			t.Errorf("%s did not receive its vitals", m.Name)
		}
	}
	if *far.CurrentHP != 1 || len(framesFor(far.ID, r, batches)) != 0 {
		t.Errorf("member at 300 healed: hp %d", *far.CurrentHP)
	}
	if *dead.CurrentHP != 0 || len(framesFor(dead.ID, r, batches)) != 0 {
		t.Errorf("dead member healed: hp %d", *dead.CurrentHP)
	}
	if *p.c.CurrentHP <= 40 {
		t.Fatalf("select bit 0 left the caster out: hp %d", *p.c.CurrentHP)
	}
	casterGID := enterworld.ObjectIDForCharacter(p.c)
	if vitalsFor(casterGID, framesFor(p.c.ID, r, batches)) != 1 {
		t.Fatal("caster vitals not published")
	}
}

/*
==================
TestPartyHealIsNotATimedHeal

Healing Orbit (efr dura puls heal) and Healing Cycle (dura puls heal) are
timed heals with no ported producer: neither may reach the party heal.
Group Reverse carries a heal block but resurrects.
==================
*/
func TestPartyHealIsNotATimedHeal(t *testing.T) {
	for _, code := range []string{"SKILL_EU_CLERIC_HEALA_CYCLE_A_01", "SKILL_EU_CLERIC_HEALA_CYCLE_B_01"} {
		if skill := shippedOffense(t, code); skill.Recovery.PartyHealPinned || skill.Recovery.PartyResurrectPinned {
			t.Fatalf("%s routed as a party heal: %+v", code, skill.Recovery)
		}
	}

	orbit := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_CYCLE_B_01")
	p := newSupportParty(t, orbit)
	p.rt.HandleTargetInteract(testDivision, p.c, wire.SkillAction{ActionId: orbit.ID}.Encode())
	p.rt.advanceProjectileCasts(p.clock.NowMs() + int64(orbit.ActionCastingTimeMs) + 1)
	if *p.m.CurrentHP != 1 || *p.c.CurrentHP != 40 {
		t.Fatalf("healing orbit healed once: mate %d caster %d", *p.m.CurrentHP, *p.c.CurrentHP)
	}

	reverse := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01")
	if reverse.Recovery.PartyHealPinned || !reverse.Recovery.PartyResurrectPinned {
		t.Fatalf("group reverse %+v", reverse.Recovery)
	}
}

/*
==================
TestGroupReverseProposesOnlyToTheDead

Group Reverse is efr[1,1,250,8,0,4] heal resu: select 4 leaves the caster
out and resu admits dead members. Only the dead member in range receives
the 0x3393 {4, caster} prompt; the living one, the dead one out of range
and the caster are neither healed nor prompted, and a yes revives with the
heal block's vitals.
==================
*/
func TestGroupReverseProposesOnlyToTheDead(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01")
	affordable(&skill)
	p := newSupportParty(t, skill, 100, 400)
	dead, alive, farDead := p.mates[0], p.mates[1], p.mates[2]
	*dead.CurrentHP, *alive.CurrentHP, *farDead.CurrentHP = 0, 100, 0
	casterGID := enterworld.ObjectIDForCharacter(p.c)
	consent := p.rt.ResurrectionConsent()

	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	prompt := framesFor(dead.ID, r, batches)
	if len(prompt) != 1 || prompt[0].Opcode != opInvitationProposal || len(prompt[0].Payload) != 5 ||
		prompt[0].Payload[0] != resurrectionProposalType ||
		binary.LittleEndian.Uint32(prompt[0].Payload[1:]) != casterGID {
		t.Fatalf("dead member prompt %+v", prompt)
	}
	if enterworld.CharacterAlive(dead) || !consent.HasPendingInvite(testDivision, dead.Name) {
		t.Fatal("the cast revived instead of proposing")
	}
	if len(framesFor(alive.ID, r, batches)) != 0 || *alive.CurrentHP != 100 ||
		consent.HasPendingInvite(testDivision, alive.Name) {
		t.Fatal("living member was offered a revival or healed")
	}
	if len(framesFor(farDead.ID, r, batches)) != 0 || consent.HasPendingInvite(testDivision, farDead.Name) {
		t.Fatal("dead member out of range was offered a revival")
	}
	if *p.c.CurrentHP != 40 {
		t.Fatalf("caster healed by a resurrection: hp %d", *p.c.CurrentHP)
	}

	maxHP, maxMP, _, _ := p.rt.playerKeeperVitals(testDivision, dead)
	*dead.CurrentMP = 0
	consent.ApplyConsent(nil, testDivision, dead, 1, 1)
	if !enterworld.CharacterAlive(dead) || *dead.CurrentHP != min(maxHP, 1+763) || *dead.CurrentMP != min(maxMP, 763) {
		t.Fatalf("revived hp %d mp %d", *dead.CurrentHP, *dead.CurrentMP)
	}
}

/*
==================
TestRaveMelodyConvertsHPToMP

Rave Melody's shape: a flat MP heal paid with a flat HP cost. The level-1
fixture holds 200 HP, below the shipped 495, so the shipped row keeps its
program and the test scales its two words by the same 1.2 ratio. The HP
check refuses 0x3013 below the cost (58E1B6); the charge (58312C) and the
MP recovery publish one 0x33A6, also when MP was already full.
==================
*/
func TestRaveMelodyConvertsHPToMP(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_ABNORMALTIME_A_01")
	if !skill.Recovery.SelfFlatPinned || skill.Consumption.HP != 495 || skill.Heal.MP != 594 ||
		skill.ActionCastingTimeMs != 0 {
		t.Fatalf("rave melody %+v cost %+v heal %+v", skill.Recovery, skill.Consumption, skill.Heal)
	}
	skill.Consumption.HP, skill.Heal.MP = 50, 60

	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	gid := enterworld.ObjectIDForCharacter(c)
	request := wire.SkillAction{ActionId: skill.ID}.Encode()

	c.CurrentHP, c.CurrentMP = testInt64(49), testInt64(10)
	refused := rt.HandleTargetInteract(testDivision, c, request)
	if len(refused.Frames) == 0 || refused.Frames[0].Payload[0] != 2 || refused.Frames[0].Payload[1] != 0x13 ||
		*c.CurrentHP != 49 || *c.CurrentMP != 10 {
		t.Fatalf("HP below cost: %+v hp %d mp %d", refused.Frames, *c.CurrentHP, *c.CurrentMP)
	}

	c.CurrentHP = testInt64(150)
	r := rt.HandleTargetInteract(testDivision, c, request)
	if r.DiagnosticRefusal != "" || r.Frames[0].Payload[0] == 2 {
		t.Fatalf("rave melody refused %q %+v", r.DiagnosticRefusal, r.Frames)
	}
	if *c.CurrentHP != 100 || *c.CurrentMP != 70 {
		t.Fatalf("hp %d mp %d, want 100 70", *c.CurrentHP, *c.CurrentMP)
	}
	if vitalsFor(gid, r.ActorPrivate) != 1 {
		t.Fatalf("vitals not published: %+v", r.ActorPrivate)
	}

	clock.Advance(time.Duration(skill.CoolTimeMs+skill.ActionDurationMs+1) * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	_, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentHP, c.CurrentMP = testInt64(150), testInt64(maxMP)
	full := rt.HandleTargetInteract(testDivision, c, request)
	if full.DiagnosticRefusal != "" || full.Frames[0].Payload[0] == 2 || *c.CurrentHP != 100 {
		t.Fatalf("full MP cast %q %+v hp %d", full.DiagnosticRefusal, full.Frames, *c.CurrentHP)
	}
	if vitalsFor(gid, full.ActorPrivate) != 1 {
		t.Fatalf("HP charge not published with MP full: %+v", full.ActorPrivate)
	}
}

/*
==================
castVitals

The HP and MP of the last full 0x33A6 that names gid:
[u32 gid][u16 source][u8 mask 3][u32 hp][u32 mp].
==================
*/
func castVitals(t *testing.T, gid uint32, frames []wire.Frame) (hp, mp uint32) {
	t.Helper()
	for _, f := range frames {
		if f.Opcode != simulation.OpVitalsUpdate || len(f.Payload) < 4 ||
			binary.LittleEndian.Uint32(f.Payload) != gid {
			continue
		}
		if len(f.Payload) != vitalsRefreshLen || f.Payload[6] != vitalsMaskHPMP {
			t.Fatalf("not a full HP/MP refresh: %x", f.Payload)
		}
		hp, mp = binary.LittleEndian.Uint32(f.Payload[7:]), binary.LittleEndian.Uint32(f.Payload[11:])
	}
	return hp, mp
}

const (
	vitalsRefreshLen = 15   // the HP and MP refresh body
	vitalsMaskHPMP   = 0x03 // update mask: HP and MP

	// fixtureCasterMP sits below the level-1 fixture's 200 MP maximum and
	// above fixtureMPCostCeiling, so a charge shows in the gauge.
	fixtureCasterMP = 150
)

/*
==================
TestResurrectionOnTheLivingCasterIsRefused

Grad Reverse cast on the caster's own living gid once refilled its HP and
MP. Reverse and Grad Reverse select corpses (column 33), so the common
permission predicate refuses a living target, the caster included, with
0x3006 before any charge: nothing is healed, paid or proposed.
==================
*/
func TestResurrectionOnTheLivingCasterIsRefused(t *testing.T) {
	const (
		castRefused       = 2    // B070 result byte of a refused cast
		targetRefusalCode = 0x06 // low byte of 0x3006
	)
	for _, code := range []string{
		"SKILL_EU_CLERIC_REBIRTHA_TARGET_A_01",
		"SKILL_EU_CLERIC_REBIRTHA_TARGET_B_01",
	} {
		skill := shippedOffense(t, code)
		if !skill.Abnormal.AdmitDeadParty || !skill.Targets.DeadBody || !skill.Heal.Present {
			t.Fatalf("%s resu %+v targets %+v", code, skill.Abnormal, skill.Targets)
		}
		affordable(&skill)
		t.Run(code, func(t *testing.T) {
			p := newSupportPair(t, skill)
			*p.c.CurrentMP = fixtureCasterMP
			request := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.c)}

			r := p.rt.HandleTargetInteract(testDivision, p.c, request.Encode())
			if len(r.Frames) == 0 || r.Frames[0].Payload[0] != castRefused || r.Frames[0].Payload[1] != targetRefusalCode {
				t.Fatalf("living caster %q %+v, want 0x3006", r.DiagnosticRefusal, r.Frames)
			}
			if *p.c.CurrentHP != 40 || *p.c.CurrentMP != fixtureCasterMP {
				t.Fatalf("caster hp %d mp %d, want 40/%d untouched", *p.c.CurrentHP, *p.c.CurrentMP, fixtureCasterMP)
			}
			if p.rt.ResurrectionConsent().HasPendingInvite(testDivision, p.c.Name) {
				t.Fatal("the living caster was offered a revival")
			}
		})
	}
}

/*
==================
TestGroupReversePublishesTheCastersCharge

Group Reverse heals nobody at the cast, but it charges the caster's MP:
the caster's 0x33A6 carries that charge at the cast, not at the next
regeneration tick.
==================
*/
func TestGroupReversePublishesTheCastersCharge(t *testing.T) {
	for _, code := range []string{
		"SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01",
		"SKILL_EU_CLERIC_REBIRTHA_GROUP_B_01",
	} {
		skill := shippedOffense(t, code)
		affordable(&skill)
		t.Run(code, func(t *testing.T) {
			p := newSupportParty(t, skill)
			casterGID := enterworld.ObjectIDForCharacter(p.c)
			*p.c.CurrentMP = fixtureCasterMP
			mpBefore := *p.c.CurrentMP

			r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
			if *p.c.CurrentMP >= mpBefore {
				t.Fatalf("caster mp %d, want the cost paid from %d", *p.c.CurrentMP, mpBefore)
			}
			toCaster := append(framesFor(p.c.ID, r, batches), r.ActorPrivate...)
			if vitalsFor(casterGID, toCaster) != 1 {
				t.Fatalf("caster charge not published: %+v %+v", r, batches)
			}
			if _, mp := castVitals(t, casterGID, toCaster); int64(mp) != *p.c.CurrentMP {
				t.Fatalf("published mp %d, want %d", mp, *p.c.CurrentMP)
			}
			if vitalsFor(casterGID, framesFor(p.m.ID, r, batches)) != 0 {
				t.Fatal("the caster's private vitals reached the mate")
			}
		})
	}
}

/*
==================
TestTargetedHealBracketNamesTheCaster

Healing on a mate heals the mate, and the opening B245 and releasing B505
name the caster, as the shipped support owner has always sent them: what a
targeted heal's bracket names natively is not established, so it is left
alone. The caster's charge is not published at the cast either.
==================
*/
func TestTargetedHealBracketNamesTheCaster(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	if skill.ActionCastingTimeMs == 0 {
		t.Fatalf("%s has no casting time", skill.Codename)
	}
	p := newSupportPair(t, skill)
	casterGID, mateGID := enterworld.ObjectIDForCharacter(p.c), enterworld.ObjectIDForCharacter(p.m)
	*p.c.CurrentMP = fixtureCasterMP

	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: mateGID})
	if *p.m.CurrentHP <= 1 {
		t.Fatalf("mate not healed: hp %d", *p.m.CurrentHP)
	}
	if *p.c.CurrentMP >= fixtureCasterMP {
		t.Fatalf("caster mp %d, want the cost paid from %d", *p.c.CurrentMP, fixtureCasterMP)
	}

	if open, release := bracketTargets(t, casterGID, r, batches); open != casterGID || release != casterGID {
		t.Fatalf("bracket names %d and %d, want the caster %d", open, release, casterGID)
	}

	if vitalsFor(casterGID, append(framesFor(p.c.ID, r, batches), r.ActorPrivate...)) != 0 {
		t.Fatalf("caster charge published: %+v %+v", r, batches)
	}
}

/*
==================
bracketTargets

The target the opening B245 names and the one the releasing B505 names:
B245 [ok][result][skill u32][caster u32][token u32][target u32], B505
[mode 1][token u32][target u32]. The opening must come from casterGID.
==================
*/
func bracketTargets(t *testing.T, casterGID uint32, r OpResult, batches []simulation.DivisionFrames) (open, release uint32) {
	t.Helper()
	opening := r.Broadcast[0]
	if opening.Opcode != wire.OpSkillCastResult || len(opening.Payload) < 18 ||
		binary.LittleEndian.Uint32(opening.Payload[6:]) != casterGID {
		t.Fatalf("opening bracket %x is not the caster's", opening.Payload)
	}
	open = binary.LittleEndian.Uint32(opening.Payload[14:])

	released := false
	for _, batch := range batches {
		for _, f := range batch.Frames {
			if f.Opcode != wire.OpSkillEffectControl || len(f.Payload) < 9 || f.Payload[0] != 1 {
				continue
			}
			if released && binary.LittleEndian.Uint32(f.Payload[5:]) != release {
				t.Fatalf("releases disagree on the target: %x", f.Payload)
			}
			released = true
			release = binary.LittleEndian.Uint32(f.Payload[5:])
		}
	}
	if !released {
		t.Fatalf("no release in %+v", batches)
	}
	return open, release
}

/*
==================
TestResurrectionBracketNamesTheCaster

Reverse on a dead mate proposes the revival, and its B245 and release B505
still name the caster, as they always have: what a revival names there is
not established, so it is left alone.
==================
*/
func TestResurrectionBracketNamesTheCaster(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_TARGET_A_01")
	if skill.ActionCastingTimeMs == 0 {
		t.Fatalf("%s has no casting time", skill.Codename)
	}
	p := newSupportPair(t, skill)
	casterGID, mateGID := enterworld.ObjectIDForCharacter(p.c), enterworld.ObjectIDForCharacter(p.m)
	*p.m.CurrentHP = 0

	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: mateGID})
	if !p.rt.ResurrectionConsent().HasPendingInvite(testDivision, p.m.Name) {
		t.Fatal("the dead mate was not proposed a revival")
	}
	if open, release := bracketTargets(t, casterGID, r, batches); open != casterGID || release != casterGID {
		t.Fatalf("bracket names %d and %d, want the caster %d", open, release, casterGID)
	}
}
