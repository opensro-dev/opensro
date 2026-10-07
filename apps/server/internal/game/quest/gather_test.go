/*
===========================================================================

gather_test.go - Ivy tool drops, gathering and trap supply lifecycle

Exercise real item planning with controlled native random boundaries. A tool
is consumed only by the action caller; material awards follow the timer.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
gatherFixture

Accept through the shared active-prerequisite predicate and NPC contract.
================
*/
func gatherFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := captureCatalogRuntime(t)
	c := questCharacter()
	*c.Level = 40
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	def, exists := rt.Defs.ByCodename(ivyMaterialQuest)
	if !exists {
		t.Fatal("material quest absent")
	}
	if _, err := rt.StartQuest(c, ivyMaterialQuest); err == nil {
		t.Fatal("material quest admitted without its active parent")
	}
	c.CompletedQuestIds = append([]uint32(nil), def.RequiredActiveQuestIDs...)
	if _, err := rt.StartQuest(c, ivyMaterialQuest); err == nil {
		t.Fatal("completed parent substituted for an active quest")
	}
	c.CompletedQuestIds = nil
	for _, id := range def.RequiredActiveQuestIDs {
		parent, _ := rt.Defs.ByRefID(id)
		c.ActiveQuests = append(c.ActiveQuests, BuildActiveQuestRecord(parent, 0))
	}
	if _, err := rt.StartQuest(c, ivyMaterialQuest); err != nil {
		t.Fatal(err)
	}
	rt.CaptureRoll = func() (uint32, error) { return 50, nil }
	return rt, c, def
}

/*
================
TestGatherCancellationCannotAwardOrCancelAnotherIdentity

The acknowledged cancellation removes the admitted job before its next pulse.
Malformed and foreign quest identities leave that job intact.
================
*/
func TestGatherCancellationCannotAwardOrCancelAnotherIdentity(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := gatherFixture(t)
	center := simulation.Spawn{RegionID: ivyGatherRegion, X: 253, Y: 85, Z: 449}
	frames, admitted := rt.BeginItemUse(c, ivyKnife, center, 0)
	if !admitted || len(frames) != 1 || frames[0].Opcode != OpQuestGatherStart || len(frames[0].Payload) != 5 {
		t.Fatal("missing native countdown", frames)
	}
	for _, request := range [][]byte{{1}, wire.NewWriter(4).U32(def.RefID + 1).Payload()} {
		result, err := rt.HandleGatherCancel(c, request)
		if err != nil || result.Frames[0].Payload[0] != 2 {
			t.Fatal("invalid cancellation admitted", result, err)
		}
	}
	result, err := rt.HandleGatherCancel(c, wire.NewWriter(4).U32(def.RefID).Payload())
	if err != nil || result.Frames[0].Opcode != OpQuestGatherCancelReply || result.Frames[0].Payload[0] != 1 {
		t.Fatal("matching cancellation refused", result, err)
	}
	for second := int64(1); second <= ivyGatherSeconds; second++ {
		rt.AdvanceItemUse(c, second*questSecondMs)
	}
	if captureItemCount(c, ivyVine) != 0 {
		t.Fatal("canceled gathering awarded material")
	}
}

/*
================
TestGatherAdmissionCountdownAndDisconnect

Native 49 fails and 50 succeeds. No early, repeated, canceled or offline
countdown may award material.
================
*/
func TestGatherAdmissionCountdownAndDisconnect(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _ := gatherFixture(t)
	center := simulation.Spawn{RegionID: ivyGatherRegion, X: 253, Y: 85, Z: 449}
	outside := center
	outside.X += ivyGatherRadius + 1
	if _, ok := rt.BeginItemUse(c, ivyKnife, outside, 0); ok {
		t.Fatal("knife admitted outside Pond Ruins")
	}
	if _, ok := rt.BeginItemUse(c, ivyKnife, center, 0); !ok {
		t.Fatal("knife refused in the gathering area")
	}
	if _, ok := rt.BeginItemUse(c, ivyKnife, center, 0); ok {
		t.Fatal("second knife admitted during the existing countdown")
	}
	for second := int64(1); second < ivyGatherSeconds; second++ {
		rt.AdvanceItemUse(c, second*questSecondMs)
		if captureItemCount(c, ivyVine) != 0 {
			t.Fatal("material awarded before ten timer pulses")
		}
	}
	rt.AdvanceItemUse(c, ivyGatherSeconds*questSecondMs)
	if captureItemCount(c, ivyVine) != 1 {
		t.Fatal("successful gathering omitted its material")
	}
	rt.AdvanceItemUse(c, 100000)
	if captureItemCount(c, ivyVine) != 1 {
		t.Fatal("completed countdown paid twice")
	}
	if _, ok := rt.BeginItemUse(c, ivyKnife, center, 200000); !ok {
		t.Fatal("next gathering attempt refused")
	}
	rt.ForgetItemUse(c)
	for second := int64(1); second <= ivyGatherSeconds; second++ {
		rt.AdvanceItemUse(c, 200000+second*questSecondMs)
	}
	if captureItemCount(c, ivyVine) != 1 {
		t.Fatal("disconnected timer awarded material")
	}
	rt.CaptureRoll = func() (uint32, error) { return 49, nil }
	if _, ok := rt.BeginItemUse(c, ivyKnife, center, 300000); !ok {
		t.Fatal("failure-boundary attempt refused before the roll")
	}
	for second := int64(1); second <= ivyGatherSeconds; second++ {
		rt.AdvanceItemUse(c, 300000+second*questSecondMs)
	}
	if captureItemCount(c, ivyVine) != 1 {
		t.Fatal("native failure roll awarded material")
	}
}

/*
================
TestIvyToolDropCompletionAndDailyResupply

Soldiers drop knives, completion removes surplus tools and grants five traps.
The completed material quest remains an NPC supplier while Ivy 1 is active.
================
*/
func TestIvyToolDropCompletionAndDailyResupply(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := gatherFixture(t)
	day := uint16(7)
	rt.CalendarNow = func() calendar.Value { return calendar.Value{Day: day} }
	drops := rt.MonsterDrops(c, "MOB_AM_SOLDIER", 0, func() (uint32, error) { return 1, nil })
	if len(drops) != 1 || drops[0].Codename != ivyKnife {
		t.Fatal("material quest did not drop its native tool", drops)
	}
	rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{
		{Codename: ivyVine, Count: 20}, {Codename: ivyKnife, Count: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.MissionInventory = rows
	rt.InventoryUpdater()(c)
	if _, err := rt.AdvanceNpcQuest(c, ivyMaterialQuest, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	supply, _ := captureSupplyForQuest(ivyMaterialQuest)
	if captureItemCount(c, ivyKnife) != 0 || captureItemCount(c, ivyVine) != 0 ||
		captureItemCount(c, supply.item) != captureSupplyCount {
		t.Fatal("completion did not exchange materials and discard unused tools")
	}
	c.MissionInventory = nil
	if option, ok := rt.captureSupplyOption(c, def, def.StartNpcCodename); !ok || !option.Informational {
		t.Fatal("completion failed to spend today's trap supply")
	}
	day++
	code := captureSupplyPrefix + ivyMaterialQuest
	if _, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename); err != nil {
		t.Fatal(err)
	}
	// Ivy spends its allowance at confirmation, so abandoning the prompt
	// still permits a new prompt on the same day.
	token, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, supply.item) != captureSupplyCount {
		t.Fatal("completed material quest did not supply replacement traps")
	}
}
