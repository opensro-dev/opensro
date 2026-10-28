/*
===========================================================================

petperiodic_admit_test.go - pet, periodic and source lanes admit on facts

The pet tick, the persistent-effect pulse and the crow's recipient walk
detached whole character records - inventory included - for refusals that
weigh a handful of fields, and the COS/monster abnormal lanes detached a
killer record no callback on those lanes ever reads (#1943). The verdicts
stay; the refused copies go. Landing behaviour stays covered by the lane
suites (pet motion, periodic pulses, tailwind party, COS abnormal).
================
*/

package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petPeriodicFatten

Give a character a full inventory so one detached copy is expensive.
================
*/
func petPeriodicFatten(c *enterworld.Character) {
	// Bag slots only: equipment and job-suit slots keep their verdicts.
	rows := make([]enterworld.InventoryRow, 0, 48)
	for slot := 0; slot < 48; slot++ {
		rows = append(rows, enterworld.InventoryRow{Slot: int64(21 + slot), RefObjID: 62, StackCount: 1000,
			MagicOptions: []uint64{uint64(slot), uint64(slot + 1)}})
	}
	c.MissionInventory = append(c.MissionInventory, rows...)
}

/*
================
TestAdvancePetRefusedCompanionSkipsOwnerCopy

A bound pet follows while it can act. A dead, unsummoned, mounted or
deletion-pending companion - or a fallen owner - is refused on the live
record and never pays for the owner's detached copy.
================
*/
func TestAdvancePetRefusedCompanionSkipsOwnerCopy(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.BindPetSession(testDivision, c, 101)
	petPeriodicFatten(c)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: gid}

	rt.advancePet(key, 1000)
	if state := rt.petSessionFor(testDivision, c.Name, gid); state == nil || state.follower == nil {
		t.Fatal("healthy pet did not take up its follow")
	}

	c.ActiveCOS.CurrentHP = 0
	rt.advancePet(key, 1100) // settle: combat cancels, the follower stops and clears
	refused := testing.AllocsPerRun(20, func() { rt.advancePet(key, 1200) })
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	t.Logf("dead pet tick %.1f allocs vs owner copy %.1f", refused, copyAllocs)
	if refused >= copyAllocs/2 {
		t.Fatalf("dead pet tick allocs %.1f, want under half the %.1f-alloc owner copy", refused, copyAllocs)
	}
	if frames := rt.advancePet(key, 1200); len(frames) != 0 {
		t.Fatal("dead pet produced frames", frames)
	}

	c.ActiveCOS.CurrentHP = 100
	c.ActiveCOS.Summoned = false
	if frames := rt.advancePet(key, 1300); len(frames) != 0 {
		t.Fatal("unsummoned pet produced frames", frames)
	}
	c.ActiveCOS.Summoned = true
	c.ActiveCOS.Mounted = true
	if frames := rt.advancePet(key, 1400); len(frames) != 0 {
		t.Fatal("mounted pet produced frames", frames)
	}
	c.ActiveCOS.Mounted = false
	savedHP := c.CurrentHP
	c.CurrentHP = testInt64(0)
	if frames := rt.advancePet(key, 1500); len(frames) != 0 {
		t.Fatal("pet of a fallen owner produced frames", frames)
	}
	c.CurrentHP = savedHP
	c.DeletePending = true
	if frames := rt.advancePet(key, 1600); len(frames) != 0 {
		t.Fatal("pet of a deletion-pending owner produced frames", frames)
	}
}

/*
================
TestAdvancePetBagRowCompanionAdmitsAndRefuses

Production pets live as summon records in bag rows, not parked on
ActiveCOS (the #1963 regression class). The tick's admission resolves
the companion through the same bag walk: a living summoned row pet
takes up its follow, and once it falls the refusal is read off the live
row without detaching the owner.
================
*/
func TestAdvancePetBagRowCompanionAdmitsAndRefuses(t *testing.T) {
	c := testCharacter()
	c.ID, c.Name = 77, "bagowner"
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	const petGID = uint32(60001)
	pet := &enterworld.CharacterCOS{GID: petGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, Summon: pet})
	rt.BindPetSession(testDivision, c, 101)
	petPeriodicFatten(c)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: petGID}

	rt.advancePet(key, 1000)
	if state := rt.petSessionFor(testDivision, c.Name, petGID); state == nil || state.follower == nil {
		t.Fatal("bag-row pet did not take up its follow")
	}
	pet.CurrentHP = 0
	rt.advancePet(key, 1100) // settle: combat cancels, the follower stops and clears
	refused := testing.AllocsPerRun(20, func() { rt.advancePet(key, 1200) })
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	t.Logf("dead bag-row pet tick %.1f allocs vs owner copy %.1f", refused, copyAllocs)
	if refused >= copyAllocs/2 {
		t.Fatalf("dead bag-row pet tick allocs %.1f, want under half the %.1f-alloc owner copy", refused, copyAllocs)
	}
	if frames := rt.advancePet(key, 1200); len(frames) != 0 {
		t.Fatal("dead bag-row pet produced frames", frames)
	}
}

/*
================
TestPeriodicDeadSourceSkipsRecordCopy

A living source's pulse lands; once the source falls, the next due pulse
is refused on its live facts and its fat record is never detached.
================
*/
func TestPeriodicDeadSourceSkipsRecordCopy(t *testing.T) {
	rt, clock, c, target := periodicFixture(t, 10000)
	start := clock.NowMs()
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode())
	rt.ClearCombatIntent(testDivision, c.Name)
	rt.drainSkillFinalizes(start + 501)
	rt.advancePeriodicEffects(start + 2000)
	before, _ := rt.characterMonster(testDivision, c, target.Gid)
	if before.CurrentHP >= target.CurrentHP {
		t.Fatal("healthy source pulse did not land")
	}

	*c.CurrentHP = 0
	petPeriodicFatten(c)
	steps := rt.periodicEffects.Frame(start + 4000)
	if len(steps) != 1 || !steps[0].Pulse {
		t.Fatalf("due steps %+v", steps)
	}
	rt.advancePeriodicEffectInDivision(steps[0], start+4000)
	after, _ := rt.characterMonster(testDivision, c, target.Gid)
	if after.CurrentHP != before.CurrentHP {
		t.Fatal("dead source pulsed")
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	refused := testing.AllocsPerRun(20, func() { rt.advancePeriodicEffectInDivision(steps[0], start+4000) })
	t.Logf("dead source pulse %.1f allocs vs record copy %.1f", refused, copyAllocs)
	if refused >= copyAllocs/2 {
		t.Fatalf("dead source pulse allocs %.1f, want under half the %.1f-alloc record copy", refused, copyAllocs)
	}
}

/*
================
TestCrowSupportDeadMemberSkipsCopy

A dead party member is refused on its live facts: it leaves the crow's
recipient set without its fat record ever being detached. A living
member is still admitted and copied as before.
================
*/
func TestCrowSupportDeadMemberSkipsCopy(t *testing.T) {
	rt, clock, c, _, _ := tailwindFixture(t, 3)
	step, ok := rt.elementalCompanion(testDivision, c, c.ActiveCOS.GID, "Crow", clock.NowMs())
	if !ok {
		t.Fatal("crow fixture not eligible")
	}
	live := nearbyCharacter(rt, c, 21, "crow-live", 50)
	dead := nearbyCharacter(rt, c, 22, "crow-dead", 50)
	petPeriodicFatten(dead)
	// nearbyCharacter shallow-copies the owner, HP pointer included: give
	// the dead member its own HP cell or the write lands on the owner too.
	dead.CurrentHP = testInt64(0)
	rt.petSessions[petOwnerKey{division: testDivision, name: live.Name}] = &petSession{character: live, ready: true}
	rt.petSessions[petOwnerKey{division: testDivision, name: dead.Name}] = &petSession{character: dead, ready: true}
	members := []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(live), enterworld.ObjectIDForCharacter(dead)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: members}} }

	recipients := rt.crowSupportRecipients(step, true, true)
	if recipients[enterworld.ObjectIDForCharacter(dead)] != nil {
		t.Fatal("dead member received crow support")
	}
	if recipients[enterworld.ObjectIDForCharacter(live)] == nil {
		t.Fatal("living member lost crow support")
	}

	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, dead) })
	withDead := testing.AllocsPerRun(20, func() { rt.crowSupportRecipients(step, true, true) })
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: members[:2]}} }
	withoutDead := testing.AllocsPerRun(20, func() { rt.crowSupportRecipients(step, true, true) })
	t.Logf("crow recipients with dead member %.1f, without %.1f, member copy %.1f", withDead, withoutDead, copyAllocs)
	if delta := withDead - withoutDead; delta >= copyAllocs/2 {
		t.Fatalf("dead member costs %.1f allocs, want under half its %.1f-alloc copy", delta, copyAllocs)
	}
}

/*
================
TestCaptureAbnormalSourceLifeSkipsKillerCopy

The life capture agrees with the full capture on existence and death for
monster, player and COS sources, carries the monster pointer the COS
shield test reads, and never detaches a killer record. The full capture
keeps its killer for the player lane (guildwar_test.go pins it).
================
*/
func TestCaptureAbnormalSourceLifeSkipsKillerCopy(t *testing.T) {
	rt, _, c, mob := newCombatTestRuntime(t, 100000)

	life := rt.captureAbnormalSourceLife(testDivision, mob.Gid)
	full := rt.captureAbnormalSource(testDivision, mob.Gid)
	if !life.exists || life.dead || life.killer.monster == nil || life.killer.player != nil ||
		life.exists != full.exists || life.dead != full.dead {
		t.Fatalf("monster source life %+v full %+v", life, full)
	}

	equipCombatTestPet(t, rt, c, cosBandAttackPet)
	pet := c.ActiveCOS
	pet.Level = 7
	life = rt.captureAbnormalSourceLife(testDivision, pet.GID)
	full = rt.captureAbnormalSource(testDivision, pet.GID)
	if !life.exists || life.dead || life.killer.player != nil ||
		full.killer.player == nil || full.killer.player.ID != c.ID || full.killer.strikerLevel != 7 {
		t.Fatalf("pet source life %+v full %+v", life, full)
	}
	pet.CurrentHP = 0
	if life = rt.captureAbnormalSourceLife(testDivision, pet.GID); !life.exists || !life.dead {
		t.Fatalf("dead pet source %+v", life)
	}
	if life = rt.captureAbnormalSourceLife(testDivision, 424242); life.exists || life.dead || life.killer.player != nil {
		t.Fatalf("vanished source %+v", life)
	}

	petPeriodicFatten(c)
	*c.CurrentHP = 0
	gid := enterworld.ObjectIDForCharacter(c)
	life = rt.captureAbnormalSourceLife(testDivision, gid)
	full = rt.captureAbnormalSource(testDivision, gid)
	if !life.exists || !life.dead || life.killer.player != nil || full.killer.player == nil {
		t.Fatalf("player source life %+v full %+v", life, full)
	}
	fullAllocs := testing.AllocsPerRun(20, func() { rt.captureAbnormalSource(testDivision, gid) })
	lifeAllocs := testing.AllocsPerRun(20, func() { rt.captureAbnormalSourceLife(testDivision, gid) })
	t.Logf("life capture %.1f allocs vs full capture %.1f", lifeAllocs, fullAllocs)
	if lifeAllocs >= fullAllocs/2 {
		t.Fatalf("life capture allocs %.1f, want under half the %.1f-alloc full capture", lifeAllocs, fullAllocs)
	}
}
