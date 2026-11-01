/*
===========================================================================

rahid_compiled_chain_test.go - a generated delivery after authored programmes

One character completes Rahid 5's seven peaks, Rahid 6's Shaitan meeting,
then the generated Rahid 7 delivery. Only Rahid 4 is completed in the
initial history. Quest progress, inventory exchanges and rewards all use
their production owners; the fixture replaces external world doors.

===========================================================================
*/

package quest

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	rahidCompiledDeliveryQuest      = "QNO_RM_OLDWOMAN_7"
	rahidCompiledDeliveryNpc        = "NPC_RM_OLDWOMAN"
	rahidCompiledDeliveryRewardItem = "ITEM_QNO_RM_OLDWOMAN_7_02"
	rahidCompiledDeliveryExp        = int64(940_000)
	rahidCompiledDeliverySkillExp   = int64(12_000)
)

/*
================
TestRahidCompiledQuestChain

The generator may resolve an authored prerequisite, but the runtime still
requires this character to finish it. Do not substitute completed IDs or
an injected necklace for either programme's actual completion path.
================
*/
func TestRahidCompiledQuestChain(t *testing.T) {
	licensed.RequireGameData(t)
	t.Setenv(progression.EnvBetaGrowth, "0")
	w := newRahid6World(t)
	rt := w.rt
	fourth, exists := rt.Defs.ByCodename("QNO_RM_OLDWOMAN_4")
	if !exists {
		t.Fatal("Rahid 4 is not loaded")
	}
	fifth, exists := rt.Defs.ByCodename(rahidQuest)
	if !exists {
		t.Fatal("Rahid 5 is not loaded")
	}
	seventh, exists := rt.Defs.ByCodename(rahidCompiledDeliveryQuest)
	if !exists {
		t.Fatal("Rahid 7 is not loaded after its authored prerequisite was resolved")
	}
	if seventh.RewardExp != rahidCompiledDeliveryExp || seventh.RewardSkillExp != rahidCompiledDeliverySkillExp {
		t.Fatalf("Rahid 7 rewards=%d/%d, want %d/%d", seventh.RewardExp, seventh.RewardSkillExp,
			rahidCompiledDeliveryExp, rahidCompiledDeliverySkillExp)
	}

	c := questCharacter()
	c.Name = "CompiledRahidChain"
	*c.Level = 100
	experience, skillPoints, skillExp := int64(0), int64(0), int64(0)
	c.Experience, c.SkillPoints, c.SkillExp = &experience, &skillPoints, &skillExp
	c.CompletedQuestIds = []uint32{fourth.RefID}
	w.division[rahid6TestDivision] = append(w.division[rahid6TestDivision], c)
	if questCompleted(c, fifth.RefID) || questCompleted(c, w.def.RefID) || questCompleted(c, seventh.RefID) {
		t.Fatal("the initial fixture already completed part of the chain")
	}

	if _, err := rt.StartQuest(c, rahidQuest); err != nil {
		t.Fatal(err)
	}
	holdItems(t, rt, c, rahidEssence, 20)
	clock := int64(100 * questSecondMs)
	for peak := range rahidPeaks {
		rahidRoll(rt, uint32(rahidPeaks[peak].chance))
		if _, admitted := rt.BeginItemUse(c, rahidEssence, rahidPeakSpawn(peak), clock); !admitted {
			t.Fatalf("Rahid 5 peak %d refused the next step", peak)
		}
		finishRahidPeak(rt, c, peak, clock)
		clock += 100 * questSecondMs
		at := activeQuestIndex(c, fifth.RefID)
		if at < 0 || int(c.ActiveQuests[at].ToolStep) != peak+1 || captureItemCount(c, rahidPile) != uint32(peak+1) {
			t.Fatalf("Rahid 5 peak %d did not advance the production gathering", peak)
		}
	}
	if _, err := rt.AdvanceNpcQuest(c, rahidQuest, fifth.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, fifth.RefID) || activeQuestIndex(c, fifth.RefID) >= 0 ||
		captureItemCount(c, rahidEssence) != 0 || captureItemCount(c, rahidPile) != 0 {
		t.Fatal("Rahid 5 did not complete and consume its gathering items")
	}

	if _, err := rt.StartQuest(c, rahid6QuestCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, rahid6BundleCodename) != 1 {
		t.Fatal("Rahid 6 acceptance did not grant its bundle")
	}
	for _, option := range rt.OptionsForNpc(c, rahid6ShiphrCodename) {
		if option.Codename == rahidCompiledDeliveryQuest {
			t.Fatal("Rahid 7 was offered before Rahid 6 completed")
		}
	}
	if _, err := rt.StartQuest(c, rahidCompiledDeliveryQuest); err == nil {
		t.Fatal("Rahid 7 accepted before Rahid 6 completed")
	}

	// The fixture's external owner doors perform admission and return;
	// the programme must still request them through its travel/event flow.
	invite := rt.InstanceInvite
	rt.InstanceInvite = func(leader *enterworld.Character, call InstanceInviteCall) InstanceInviteOutcome {
		outcome := invite(leader, call)
		if outcome.Opened && outcome.LeaderIn {
			point := rahidPeakSpawn(len(rahidPeaks) - 1)
			packed := uint32(outcome.Lease.ID)
			region, angle := int64(call.Entry.RegionID), int64(call.Entry.Angle)
			x, y, z := call.Entry.X, call.Entry.Y, call.Entry.Z
			leader.World = &enterworld.CharacterWorld{
				PackedInstance: &packed,
				Spawn: &enterworld.WorldSpawn{
					RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle,
				},
				InstanceReturn: &domain.InstanceReturnAnchor{
					PackedInstance: domain.DefaultWorldInstance, RegionID: point.RegionID,
					X: float32(point.X), Y: float32(point.Y), Z: float32(point.Z), Angle: point.Angle,
				},
			}
		}
		return outcome
	}
	returnCharacter := rt.InstanceReturn
	rt.InstanceReturn = func(character *enterworld.Character) bool {
		if character.World == nil || character.World.InstanceReturn == nil || !returnCharacter(character) {
			return false
		}
		anchor := character.World.InstanceReturn
		packed := anchor.PackedInstance
		region, angle := int64(anchor.RegionID), int64(anchor.Angle)
		x, y, z := float64(anchor.X), float64(anchor.Y), float64(anchor.Z)
		character.World = &enterworld.CharacterWorld{
			PackedInstance: &packed,
			Spawn: &enterworld.WorldSpawn{
				RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle,
			},
		}
		return true
	}
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	if _, err := rt.AdvanceNpcQuest(c, rahid6TravelPrefix+rahid6QuestCodename, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if len(w.invites) != 1 || w.invites[0].Definition != rahid6Definition ||
		domain.CharacterWorldInstance(c) != uint32(w.lease.ID) {
		t.Fatal("Rahid 6 travel did not admit the character into its meeting")
	}
	for _, kind := range []InstanceEventKind{InstanceEventSetup, InstanceEventEnter, InstanceEventActivated} {
		rt.DispatchInstanceEvent(InstanceEvent{
			Kind: kind, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: c.Name,
		})
	}
	*w.nowMs += rahid6SpawnDelayMs
	w.update()
	if len(w.spawned) != 1 || w.spawned[0].Codename != rahid6TahometCodename || w.spawned[0].Population != w.lease {
		t.Fatal("Rahid 6 did not spawn Tahomet in the admitted population")
	}
	for range rahid6ConvoLines {
		*w.nowMs += rahid6ConvoStepMs
		w.update()
	}
	if !hasNotice(w.pushed[c.Name], rahid6SymBattleBegun) {
		t.Fatal("Rahid 6 did not reach its battle phase")
	}
	rt.KillUpdater()(c, rahid6TahometCodename, 0)
	w.update()
	*w.nowMs += rahid6KillGraceMs
	w.update()
	if captureItemCount(c, rahid6BundleCodename) != 0 || captureItemCount(c, rahid6NecklaceCodename) != 1 ||
		len(w.returned) != 1 || w.returned[0] != c.Name || len(w.closed) != 1 || w.closed[0] != w.lease ||
		domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || c.World.InstanceReturn != nil {
		t.Fatal("Rahid 6 kill did not exchange the bundle and finish its meeting")
	}
	if questCompleted(c, w.def.RefID) {
		t.Fatal("Rahid 6 completed before reporting to Shiphr")
	}
	if _, err := rt.AdvanceNpcQuest(c, rahid6QuestCodename, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, w.def.RefID) || activeQuestIndex(c, w.def.RefID) >= 0 || captureItemCount(c, rahid6NecklaceCodename) != 0 {
		t.Fatal("Rahid 6 did not complete through its Shiphr handoff")
	}

	offered := false
	for _, option := range rt.OptionsForNpc(c, rahid6ShiphrCodename) {
		offered = offered || option.Codename == rahidCompiledDeliveryQuest
	}
	if !offered {
		t.Fatal("Rahid 7 was not offered after the same character completed Rahid 6")
	}
	expBefore, pointsBefore, skillBefore := *c.Experience, *c.SkillPoints, *c.SkillExp
	rewardBefore := captureItemCount(c, rahidCompiledDeliveryRewardItem)
	if _, err := rt.StartQuest(c, rahidCompiledDeliveryQuest); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, rahid6NecklaceCodename) != 1 {
		t.Fatal("Rahid 7 acceptance did not grant the normal delivery item")
	}
	if _, err := rt.AdvanceNpcQuest(c, rahidCompiledDeliveryQuest, rahidCompiledDeliveryNpc); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, seventh.RefID) || activeQuestIndex(c, seventh.RefID) >= 0 || captureItemCount(c, rahid6NecklaceCodename) != 0 {
		t.Fatal("Rahid 7 delivery did not consume the necklace and complete")
	}
	if got := *c.Experience - expBefore; got != rahidCompiledDeliveryExp {
		t.Fatalf("Rahid 7 EXP delta=%d, want %d", got, rahidCompiledDeliveryExp)
	}
	if got := (*c.SkillPoints-pointsBefore)*progression.SkillExpPerSP + *c.SkillExp - skillBefore; got != rahidCompiledDeliverySkillExp {
		t.Fatalf("Rahid 7 skill-EXP delta=%d, want %d", got, rahidCompiledDeliverySkillExp)
	}
	if got := captureItemCount(c, rahidCompiledDeliveryRewardItem); got != rewardBefore+1 {
		t.Fatalf("Rahid 7 reward count=%d, want %d", got, rewardBefore+1)
	}
	completed := c.Snapshot()
	if _, err := rt.StartQuest(c, rahidCompiledDeliveryQuest); err == nil {
		t.Fatal("the once-only Rahid 7 quest accepted again")
	}
	if _, err := rt.AdvanceNpcQuest(c, rahidCompiledDeliveryQuest, rahidCompiledDeliveryNpc); err == nil {
		t.Fatal("the completed Rahid 7 quest handed over twice")
	}
	if !reflect.DeepEqual(c.Snapshot(), completed) {
		t.Fatal("a repeated Rahid 7 request changed inventory, rewards or completion")
	}
}
