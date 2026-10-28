/*
===========================================================================

petadmit_test.go - per-tick pet checks copy the owner only when admitted

elementalCompanionEligibility and bondSource run per pet per tick (and
per bond plan). They copied the owner's whole character, inventory
included, before learning that the pet was the wrong kind: about
2.6 GB a minute on the live shard (#1943). Admission keeps every
verdict and skips the copy for a rejected pet.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPetChecksKeepTheirVerdictWithoutCopyingRejectedOwners

A crow owner: the crow check still passes, the wolf and bear checks
still fail, and each failing check allocates less than one copy of the
owner would.
================
*/
func TestPetChecksKeepTheirVerdictWithoutCopyingRejectedOwners(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	gid, now := c.ActiveCOS.GID, clock.NowMs()
	step, ok := rt.elementalCompanion(testDivision, c, gid, "Crow", now)
	if !ok {
		t.Fatal("crow owner lost crow eligibility")
	}
	if _, ok := rt.elementalCompanion(testDivision, c, gid, "Wolf", now); ok {
		t.Fatal("crow owner became wolf-eligible")
	}
	if _, _, _, _, ok := rt.bondSource(step.key, step.state); ok {
		t.Fatal("crow became a bond source")
	}

	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	wolfAllocs := testing.AllocsPerRun(20, func() { rt.elementalCompanion(testDivision, c, gid, "Wolf", now) })
	bondAllocs := testing.AllocsPerRun(20, func() { rt.bondSource(step.key, step.state) })
	t.Logf("allocs: one owner copy %.0f, rejected wolf check %.0f, rejected bond check %.0f", copyAllocs, wolfAllocs, bondAllocs)
	if wolfAllocs >= copyAllocs || bondAllocs >= copyAllocs {
		t.Fatalf("rejected checks still copy the owner: wolf %.0f, bond %.0f allocs; one copy is %.0f",
			wolfAllocs, bondAllocs, copyAllocs)
	}
}

/*
================
TestPetAdvancesKeepTheirVerdictsWithoutCopyingRejectedOwners

The two per-tick pet advances copied the owner before refusing the
pets they never move: advanceCrowBrood for every non-crow,
advancePetSpirit for every pet without Resolve stacks. The refusals
still refuse, the admitted advances still land, and each refused
advance allocates less than one copy of the owner would.
================
*/
func TestPetAdvancesKeepTheirVerdictsWithoutCopyingRejectedOwners(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
	now := clock.NowMs()
	if frames := rt.advanceCrowBrood(key, now); frames != nil {
		t.Fatalf("bear owner gained brood frames: %v", frames)
	}
	if frames := rt.advancePetSpirit(key, now); frames != nil {
		t.Fatalf("stackless bear gained spirit frames: %v", frames)
	}

	// The admitted advances keep their verdicts: a crow's egg still
	// hatches on schedule, and earned Resolve stacks still expire.
	crowRT, crowClock, crow, _, crowKey := tailwindFixture(t, 1)
	skill := learnPetBook(t, crow, 49)
	crowRT.castCrowBrood(testDivision, crow, crowKey.gid, skill, crowClock.NowMs())
	if len(crow.Brood) != 1 {
		t.Fatal("crow cast left no egg")
	}
	crowClock.now = crowClock.now.Add(30 * time.Second)
	crowRT.advanceCrowBrood(crowKey, crowClock.NowMs())
	if crow.Brood[0].BroodStage != 2 {
		t.Fatal("admitted crow advance lost the hatch")
	}
	// The hatched clone's own key lives in brood space: advancing it
	// as a brood source is refused, below the copy.
	broodKey := petOwnerKey{division: testDivision, name: crow.Name, gid: crow.Brood[0].GID}
	if frames := crowRT.advanceCrowBrood(broodKey, crowClock.NowMs()); frames != nil {
		t.Fatalf("brood-space gid gained brood frames: %v", frames)
	}

	bearRT, bearClock, bear, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(bearRT, bear, "COS_P_BEAR_090", 90)
	bearRT.AttackPetExpansion = true
	bearKey := petOwnerKey{division: testDivision, name: bear.Name, gid: bear.ActiveCOS.GID}
	bear.ActiveCOS.ResolveStacks = 5
	bearRT.advancePetSpirit(bearKey, bearClock.NowMs())
	if bear.ActiveCOS.ResolveStacks != 0 {
		t.Fatal("admitted spirit advance lost the expiry")
	}

	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	broodAllocs := testing.AllocsPerRun(20, func() { rt.advanceCrowBrood(key, now) })
	spiritAllocs := testing.AllocsPerRun(20, func() { rt.advancePetSpirit(key, now) })
	crowCopyAllocs := testing.AllocsPerRun(20, func() { crowRT.characterSnapshot(testDivision, crow) })
	broodSpaceAllocs := testing.AllocsPerRun(20, func() { crowRT.advanceCrowBrood(broodKey, crowClock.NowMs()) })
	t.Logf("allocs: one owner copy %.0f, refused brood advance %.0f, refused spirit advance %.0f; crow owner copy %.0f, refused brood-space advance %.0f",
		copyAllocs, broodAllocs, spiritAllocs, crowCopyAllocs, broodSpaceAllocs)
	if broodAllocs >= copyAllocs || spiritAllocs >= copyAllocs || broodSpaceAllocs >= crowCopyAllocs {
		t.Fatalf("refused advances still copy the owner: brood %.0f, spirit %.0f, brood-space %.0f allocs; one copy is %.0f (crow %.0f)",
			broodAllocs, spiritAllocs, broodSpaceAllocs, copyAllocs, crowCopyAllocs)
	}
}

/*
================
stuffOwnerInventories

Fill the owner's bag and avatar rows with content no equipment reader
resolves, on top of the equipment the fixture already wears.
================
*/
func stuffOwnerInventories(c *enterworld.Character) {
	for slot := int64(domain.EquipmentSlotEnd); slot < 45; slot++ {
		c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: slot, Codename: "ITEM_BAG_FILLER", Durability: 3})
	}
	c.AvatarInventory = &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_FILLER"}}}
}

/*
================
assertNarrowCopy

The handed-out owner copy carries equipment rows only and no avatar
inventory.
================
*/
func assertNarrowCopy(t *testing.T, what string, snapshot *enterworld.Character) {
	t.Helper()
	if snapshot == nil {
		t.Fatalf("%s: no owner copy", what)
	}
	if snapshot.AvatarInventory != nil {
		t.Fatalf("%s: copy kept the avatar inventory", what)
	}
	for _, row := range snapshot.MissionInventory {
		if row.Slot < 0 || row.Slot >= int64(domain.EquipmentSlotEnd) {
			t.Fatalf("%s: copy kept bag row %+v", what, row)
		}
	}
}

/*
================
movePetToBagRow

Park the fixture pet where production keeps a persistent pet: its
record rides in the mission row of its own item (a bag slot, Summoned
true), never in ActiveCOS, which only a summoned vehicle fills. The
pet sessions bound earlier keep pointing at the same record.
================
*/
func movePetToBagRow(c *enterworld.Character, slot int64) {
	pet := c.ActiveCOS
	c.ActiveCOS = nil
	c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{
		Slot: slot, Codename: "ITEM_PET_SCROLL", Summon: pet,
	})
}

/*
================
TestBagRowCompanionsKeepPetVerdicts

Production shape: the wolf's and the bear's records ride in bag rows
(slot 100, Summoned, ActiveCOS nil) behind a stuffed bag. The wolf
stays eligible and the bear stays a bond source with a bond plan -
both lanes resolve their pets through the owner's rows, so their
owner copies stay full (#1943).
================
*/
func TestBagRowCompanionsKeepPetVerdicts(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	gid, now := c.ActiveCOS.GID, clock.NowMs()
	movePetToBagRow(c, 100)
	stuffOwnerInventories(c)
	if _, ok := rt.wolfCompanion(testDivision, c, gid, now); !ok {
		t.Fatal("wolf in a bag row lost wolf eligibility")
	}

	bearRT, bearClock, bear, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(bearRT, bear, "COS_P_BEAR_090", 90)
	bearRT.AttackPetExpansion = true
	roarField(t, bearRT, bear, bearClock)
	skill := learnPetBook(t, bear, 26)
	bear.ActiveCOS.Training.Levels[skill.Index()] = 10
	bearGID, bearNow := bear.ActiveCOS.GID, bearClock.NowMs()
	movePetToBagRow(bear, 100)
	stuffOwnerInventories(bear)
	bearStep, ok := bearRT.elementalCompanion(testDivision, bear, bearGID, "Bear", bearNow)
	if !ok {
		t.Fatal("bear in a bag row lost eligibility")
	}
	owner, pet, _, _, ok := bearRT.bondSource(bearStep.key, bearStep.state)
	if !ok || owner == nil || pet == nil {
		t.Fatal("bear in a bag row stopped being a bond source")
	}
	if plan := bearRT.planPetBond(testDivision, bear, bearNow); plan == nil {
		t.Fatal("bear in a bag row lost its bond plan")
	}
}
