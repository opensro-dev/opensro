/*
===========================================================================

skillcure_test.go - authored cure admission, targeting and publication.

Exercise player, party and pet targets through the gameplay owners. Status
fixtures prepare source facts before mutation, matching production admission.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
shippedSkills

Keep retail-data coverage explicit; source-only environments skip this fixture.
================
*/
func shippedSkills(t *testing.T) *enterworld.TextdataSkills {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	return enterworld.NewTextdataSkills(dir)
}

/*
================
installShippedSkill

Equip the authored skill and its weapon prerequisite on the detached caster.
================
*/
func installShippedSkill(t *testing.T, rt *Runtime, c *enterworld.Character, id uint32) enterworld.SkillRow {
	t.Helper()
	row, ok := shippedSkills(t).SkillByID(id)
	if !ok || !row.Abnormal.CurePresent() {
		t.Fatalf("skill %d is not a shipped cure", id)
	}
	source := rt.deps.(*enterworld.Deps).Skills.(staticSkillSource)
	source[id] = row
	c.Skills = append(c.Skills, id)
	c.CurrentMP = testInt64(10000)
	ref := rt.deps.(*enterworld.Deps).Items.(staticItemSource)["ITEM_CH_SWORD_01_A"]
	ref.TypeIDs[3] = int64(row.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = ref.TypeFlags()
	return row
}

/*
================
seedPlayerStatus

Prepare source facts before the fixture transaction, matching game admission.
================
*/
func seedPlayerStatus(rt *Runtime, c *enterworld.Character, status abnormal.Status, duration, now int64, source uint32) {
	record := abnormal.Record{Status: status, DurationMs: uint32(duration), Level: 1, SourceGID: source}
	owner := rt.newPlayerAbnormalOwner(testDivision, c, now)
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	rt.deps.Update(c, "seed-status", func() bool {
		owner.changed = owner.block.Apply(owner, record, now)
		owner.commit()
		return true
	})
}

/*
================
TestShippedInnocentCureArms

Verify the shipped cure's admission, effect timing and status publications.
================
*/
func TestShippedInnocentCureArms(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10077)
	if row.Abnormal.CurtMask != 63 || row.Abnormal.CurtLevel != 48 || !row.Abnormal.RcurSet || row.Abnormal.Rcur != 2 {
		t.Fatalf("shipped curt/rcur %+v", row.Abnormal)
	}
	if row.Abnormal.CurlMask&63 != 0 {
		t.Fatal("shipped curl overlaps curt; the precedence fixture must be rebuilt")
	}
	var pillStatus abnormal.Status
	found := false
	for s := abnormal.Status(0); s < abnormal.SlotCount; s++ {
		if s == abnormal.Freeze || s == abnormal.Sleep || s == abnormal.Stun {
			continue
		}
		if row.Abnormal.CurlMask&int32(s.Bit()) != 0 {
			pillStatus = s
			found = true
			break
		}
	}
	if !found || pillStatus <= abnormal.Zombie {
		t.Fatalf("curl mask %d has no slot above zombie", row.Abnormal.CurlMask)
	}
	now := clock.NowMs()
	seedPlayerStatus(rt, caster, abnormal.Frostbite, 100000, now, monster.Gid)
	seedPlayerStatus(rt, caster, abnormal.Burn, 100000, now, monster.Gid)
	seedPlayerStatus(rt, caster, abnormal.Poison, 100000, now, monster.Gid)
	seedPlayerStatus(rt, caster, pillStatus, 100000, now, monster.Gid)
	rt.clearSkillFinalizes(testDivision, caster.Name)
	rolls := []uint32{0, 0, 0, 0}
	rt.CombatRoll = func() (uint32, error) {
		value := rolls[0]
		rolls = rolls[1:]
		return value, nil
	}
	result := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{ActionId: row.ID}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	block := rt.playerAbnormal(testDivision, caster.Name)
	if block == nil {
		t.Fatal("cure removed the block")
	}
	if block.Slots[abnormal.Frostbite].StartedAt != now-int64(48*250) || !block.Slots[abnormal.Frostbite].Active {
		t.Fatalf("frost cut start %d active %v", block.Slots[abnormal.Frostbite].StartedAt, block.Slots[abnormal.Frostbite].Active)
	}
	if block.Slots[abnormal.Burn].StartedAt != now || block.Slots[abnormal.Poison].StartedAt != now {
		t.Fatal("rcur touched a slot past the cap")
	}
	if block.Slots[pillStatus].Active {
		t.Fatal("pill slot was not cleared")
	}
	if !hasOpcode(result.Frames, 0x36C7) || !hasOpcode(result.Broadcast, simulation.OpVitalsUpdate) || hasOpcode(result.Broadcast, 0x36C7) {
		t.Fatalf("caster cure publication frames=%v broadcast=%v", result.Frames, result.Broadcast)
	}
}

/*
==================
TestShippedInnocentAreaCuresCasterAndParty

Innocent B's efr block is [1, 1, 300, 8, 0, 5]: TargetSelection_Party
(58BEF0) pushes the caster first because +0x14 (5) has bit 0 set, then
every living party member within 300 units (3D, adjacent sectors). curt
mask 63 covers burn and curl's mask does not, so each cured burn moves back
by exactly CurtLevel * 750 ms (410B40 burn) and stays active.
==================
*/
/*
================
TestShippedInnocentAreaCuresCasterAndParty

Area resolution must cure eligible party targets and route each private result.
================
*/
func TestShippedInnocentAreaCuresCasterAndParty(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10088)
	if row.TargetRequired || !row.Abnormal.Curt || !row.Abnormal.EffectArea.Present || row.Abnormal.EffectArea.Select != 5 || row.Abnormal.EffectArea.Radius != 300 {
		t.Fatalf("B row curt %v area %+v", row.Abnormal.Curt, row.Abnormal.EffectArea)
	}
	// B carries curt 63,152: a 114000 ms cut, so the seeded burns run 300 s to
	// stay active and expose the exact shortened start.
	cut := int64(row.Abnormal.CurtLevel) * 750
	if row.Abnormal.CurtLevel != 152 {
		t.Fatalf("B curt level %d", row.Abnormal.CurtLevel)
	}
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	seedPlayerStatus(rt, caster, abnormal.Burn, 300000, now, monster.Gid)
	solo := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{ActionId: row.ID}.Encode())
	if solo.DiagnosticRefusal != "" || len(solo.Frames) == 0 || len(solo.Frames[0].Payload) < 3 {
		t.Fatalf("solo cast refused %q %+v", solo.DiagnosticRefusal, solo.Frames)
	}
	if got := rt.playerAbnormal(testDivision, caster.Name).Slots[abnormal.Burn].StartedAt; got != now-cut {
		t.Fatalf("solo cast: caster burn start %d want %d (include-self bit)", got, now-cut)
	}
	if !hasOpcode(solo.Frames, 0x36C7) {
		t.Fatal("caster did not receive its own snapshot")
	}

	caster.OffensiveSkillCooldowns = nil
	caster.SharedSkillCooldowns = nil
	// Each target-resolution round starts with an independent admission state.
	caster.SkillActionRecoveryUntilMs = 0
	caster.CurrentMP = nil
	rt.clearSkillFinalizes(testDivision, caster.Name)
	// An equal burn never replaces the shortened one (strictly stronger only),
	// so drop the solo result before seeding the party round.
	rt.storePlayerAbnormal(testDivision, caster.Name, nil)
	seedPlayerStatus(rt, caster, abnormal.Burn, 300000, now, monster.Gid)
	deps := rt.deps.(*enterworld.Deps)
	member := func(id int64, name string, dx float64) *enterworld.Character {
		m := *caster
		m.ID, m.Name = id, name
		m.CurrentHP, m.CurrentMP = testInt64(100), testInt64(10000)
		if dx != 0 {
			// Spawn is a pointer: copy it, or the caster's own spawn moves.
			world := *caster.World
			spawn := *world.Spawn
			x := *spawn.X + dx
			spawn.X = &x
			world.Spawn = &spawn
			m.World = &world
		}
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &m)
		seedPlayerStatus(rt, &m, abnormal.Burn, 300000, now, monster.Gid)
		return &m
	}
	mate := member(4, "mate", 0)
	far := member(5, "far", 400)
	casterGID := enterworld.ObjectIDForCharacter(caster)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, enterworld.ObjectIDForCharacter(mate), enterworld.ObjectIDForCharacter(far)}}}
	}
	party := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{ActionId: row.ID}.Encode())
	if party.DiagnosticRefusal != "" {
		t.Fatal(party.DiagnosticRefusal)
	}
	if got := rt.playerAbnormal(testDivision, caster.Name).Slots[abnormal.Burn].StartedAt; got != now-cut {
		t.Fatalf("party cast: caster burn start %d want %d", got, now-cut)
	}
	if got := rt.playerAbnormal(testDivision, "mate").Slots[abnormal.Burn].StartedAt; got != now-cut {
		t.Fatalf("member in range burn start %d want %d", got, now-cut)
	}
	if got := rt.playerAbnormal(testDivision, "far").Slots[abnormal.Burn].StartedAt; got != now {
		t.Fatalf("member 400 units away was treated: start %d", got)
	}
	// Each member's snapshot goes to that member, never to the caster.
	delivered := false
	for _, recipient := range party.Recipients {
		if recipient.CharacterID == far.ID {
			t.Fatal("out-of-range member received a snapshot")
		}
		if recipient.CharacterID == mate.ID && hasOpcode(recipient.Frames, 0x36C7) {
			delivered = true
		}
	}
	if !delivered || !hasOpcode(party.Broadcast, simulation.OpVitalsUpdate) {
		t.Fatal("party cure publication", party.Frames, party.Recipients, party.Broadcast)
	}
}

/*
================
TestCureLimitZeroAndHighSlot

Exercise zero-level and high-slot cure boundaries independently of retail rows.
================
*/
func TestCureLimitZeroAndHighSlot(t *testing.T) {
	block := &abnormal.Block{}
	block.Slots[abnormal.Freeze].Active = true
	block.Slots[abnormal.Freeze].StartedAt = 50
	if block.Cure(nil, nil, &abnormal.SkillLevelCure{Mask: 1, Level: 48}, nil, 0, func() int32 { return 0 }) {
		t.Fatal("zero cap reported a change")
	}
	if block.Slots[abnormal.Freeze].StartedAt != 50 || !block.Slots[abnormal.Freeze].Active {
		t.Fatal("zero cap touched the slot")
	}
	block.Slots[abnormal.Stun].Active = true
	block.Slots[abnormal.Stun].StartedAt = 80
	block.Slots[abnormal.Stun].DurationMs = 1000
	if block.Cure(nil, nil, &abnormal.SkillLevelCure{Mask: abnormal.Stun.Bit(), Level: 48}, nil, -1, func() int32 { return 0 }) {
		t.Fatal("a slot above zombie was changed")
	}
	if block.Slots[abnormal.Stun].StartedAt != 80 || !block.Slots[abnormal.Stun].Active {
		t.Fatal("high slot was cleared")
	}
}

/*
================
TestShippedInnocentTargetResolution

Missing targets cannot fall back to the caster; pet cures use the pet block.
================
*/
func TestShippedInnocentTargetResolution(t *testing.T) {
	rt, clock, caster, monster := newCombatTestRuntime(t, 100)
	row := installShippedSkill(t, rt, caster, 10077)
	caster.Intellect = testInt64(80)
	caster.CurrentMP = nil
	now := clock.NowMs()
	seedPlayerStatus(rt, caster, abnormal.Frostbite, 100000, now, monster.Gid)
	rt.clearSkillFinalizes(testDivision, caster.Name)
	self := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(caster),
	}.Encode())
	if self.DiagnosticRefusal != "" || len(self.Frames) == 0 || len(self.Frames[0].Payload) < 3 {
		t.Fatalf("self gid cast refused %q %+v", self.DiagnosticRefusal, self.Frames)
	}
	block := rt.playerAbnormal(testDivision, caster.Name)
	cut := now - int64(48*250)
	if block == nil || block.Slots[abnormal.Frostbite].StartedAt != cut {
		t.Fatal("a target gid equal to the caster did not cure the caster")
	}

	caster.OffensiveSkillCooldowns = nil
	caster.SharedSkillCooldowns = nil
	// Each target-resolution round starts with an independent admission state.
	caster.SkillActionRecoveryUntilMs = 0
	caster.CurrentMP = nil
	rt.clearSkillFinalizes(testDivision, caster.Name)
	missing := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(caster) + 50,
	}.Encode())
	if missing.DiagnosticRefusal != "" || len(missing.Frames) == 0 || len(missing.Frames[0].Payload) < 3 {
		t.Fatalf("unresolved gid cast refused %q %+v", missing.DiagnosticRefusal, missing.Frames)
	}
	if hasOpcode(missing.Frames, 0x36C7) {
		t.Fatal("an unresolved target published a cure")
	}
	if rt.playerAbnormal(testDivision, caster.Name).Slots[abnormal.Frostbite].StartedAt != cut {
		t.Fatal("an unresolved target fell through to the caster")
	}

	caster.OffensiveSkillCooldowns = nil
	caster.SharedSkillCooldowns = nil
	// Each target-resolution round starts with an independent admission state.
	caster.SkillActionRecoveryUntilMs = 0
	caster.CurrentMP = nil
	rt.clearSkillFinalizes(testDivision, caster.Name)
	const petGID = uint32(9001)
	caster.ActiveCOS = &enterworld.CharacterCOS{GID: petGID, CurrentHP: 100, Summoned: true}
	petRecord := abnormal.Record{Status: abnormal.Frostbite, DurationMs: 100000, Level: 1, SourceGID: monster.Gid}
	petOwner := rt.newCosAbnormalOwner(testDivision, caster, now)
	petOwner.sources = rt.captureAbnormalSources(testDivision, petOwner.block, []abnormal.Record{petRecord})
	rt.deps.Update(caster, "seed-pet-status", func() bool {
		petOwner.changed = petOwner.block.Apply(petOwner, petRecord, now)
		petOwner.commit()
		return true
	})
	pet := rt.HandleTargetInteract(testDivision, caster, wire.SkillAction{
		ActionId: row.ID, HasTarget: true, TargetGid: petGID,
	}.Encode())
	if pet.DiagnosticRefusal != "" || len(pet.Frames) == 0 || len(pet.Frames[0].Payload) < 3 {
		t.Fatalf("pet cast refused %q %+v", pet.DiagnosticRefusal, pet.Frames)
	}
	stored := rt.cosAbnormal(testDivision, caster.Name, petGID)
	if stored == nil || stored.Slots[abnormal.Frostbite].StartedAt != now-int64(48*250) {
		t.Fatal("pet target was not cured")
	}
	// 4A5C60 sends 0x36C7 for players only; the client applies it to the
	// local player, so a pet's cure publishes its mask on 33A6 alone.
	if hasOpcode(pet.Frames, 0x36C7) || hasOpcode(pet.Broadcast, 0x36C7) || !hasPetMask(pet.Broadcast, petGID) {
		t.Fatalf("pet cure publication frames=%v broadcast=%v", pet.Frames, pet.Broadcast)
	}
}

/*
================
TestResuTagAdmitsDeadPartyMember

The resurrection tag changes life-state admission for an eligible party target.
================
*/
func TestResuTagAdmitsDeadPartyMember(t *testing.T) {
	rt, clock, caster, _ := newCombatTestRuntime(t, 100)
	row, ok := shippedSkills(t).SkillByID(10256)
	if !ok || !row.Abnormal.AdmitDeadParty || row.Abnormal.EffectArea.Select != 4 {
		t.Fatalf("rebirth group resu/efr %+v admit %v", row.Abnormal.EffectArea, row.Abnormal.AdmitDeadParty)
	}
	mate := *caster
	mate.ID = 4
	mate.Name = "dead"
	mate.CurrentHP = testInt64(0)
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &mate)
	casterGID := enterworld.ObjectIDForCharacter(caster)
	mateGID := enterworld.ObjectIDForCharacter(&mate)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, mateGID}}}
	}
	now := clock.NowMs()
	admitted := rt.partyCureTargets(testDivision, caster, row.Abnormal.EffectArea.Radius, false, true, now)
	if len(admitted) != 1 || admitted[0] != mateGID {
		t.Fatalf("dead member %v", admitted)
	}
	if skipped := rt.partyCureTargets(testDivision, caster, row.Abnormal.EffectArea.Radius, false, false, now); len(skipped) != 0 {
		t.Fatalf("alive check kept a dead member %v", skipped)
	}
}

/*
================
hasOpcode

Inspect wire behavior without depending on private helper names.
================
*/
func hasOpcode(frames []wire.Frame, opcode uint16) bool {
	for _, frame := range frames {
		if frame.Opcode == opcode {
			return true
		}
	}
	return false
}

// hasPetMask finds the 33A6 abnormal channel (flags 4) for one gid.
/*
================
hasPetMask

Require the shared abnormal channel to address the summoned pet's wire ID.
================
*/
func hasPetMask(frames []wire.Frame, gid uint32) bool {
	for _, f := range frames {
		if f.Opcode == simulation.OpVitalsUpdate && len(f.Payload) >= 11 && f.Payload[6] == 4 &&
			uint32(f.Payload[0])|uint32(f.Payload[1])<<8|uint32(f.Payload[2])<<16|uint32(f.Payload[3])<<24 == gid {
			return true
		}
	}
	return false
}
