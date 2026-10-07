/*
===========================================================================

rahid_chain_test.go - Rahid 2's forked offer, Rahid 3's Hotan exchange and
Rahid 4's slaves

Exercise the shipped definitions: the reply that picks feathers or waiting,
the two-page dialogue with Ahmok before the report to Bukhra, and the slaves
who only answer while Shiphr completes.

===========================================================================
*/
package quest

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
rahidChainFixture

A level-100 character that has finished every predecessor of code.
================
*/
func rahidChainFixture(t *testing.T, code string) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := captureCatalogRuntime(t)
	def, exists := rt.Defs.ByCodename(code)
	if !exists {
		t.Fatalf("%s is not loaded", code)
	}
	c := questCharacter()
	*c.Level = 100
	experience := int64(0)
	c.Experience = &experience
	c.CompletedQuestIds = append([]uint32(nil), def.RequiredQuestIDs...)
	return rt, c, def
}

/*
================
npcRow

The row npc shows for code, if any.
================
*/
func npcRow(rt *Runtime, c *enterworld.Character, code, npc string) (NpcOption, bool) {
	for _, row := range rt.OptionsForNpc(c, npc) {
		token, _, _ := parseBranchToken(strings.TrimPrefix(row.Codename, sideTalkPrefix))
		if base, _, _ := parseStageToken(token); base == code {
			return row, true
		}
	}
	return NpcOption{}, false
}

/*
================
hasNotice

Whether frames carry the quest notification for symbol.
================
*/
func hasNotice(frames []wire.Frame, symbol string) bool {
	want := questNotification(symbol)
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && string(frame.Payload) == string(want.Payload) {
			return true
		}
	}
	return false
}

/*
================
TestRahidTwoOfferForksIntoFeathersOrWaiting

Bukhra's offer carries the two replies; the bare codename is refused. The
feather branch pays scrolls for 200 feathers.
================
*/
func TestRahidTwoOfferForksIntoFeathersOrWaiting(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidChainFixture(t, "QNO_RM_OLDWOMAN_2")
	row, offered := npcRow(rt, c, def.Codename, def.StartNpcCodename)
	if !offered || len(row.Branches) != 2 || row.Branches[1].ReplySymbol != "SN_TALK_QNO_RM_OLDWOMAN_2_06" {
		t.Fatalf("offer row %+v", row)
	}
	if _, err := rt.StartQuest(c, def.Codename); err == nil {
		t.Fatal("a branching offer was accepted without a reply")
	}
	if _, err := rt.StartQuest(c, row.Branches[0].Codename); err != nil {
		t.Fatal(err)
	}
	if row, _ := npcRow(rt, c, def.Codename, def.EndNpcCodename); row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_2_04" || !row.Informational {
		t.Fatalf("feather branch not-achieved row %+v", row)
	}
	holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
	row, _ = npcRow(rt, c, def.Codename, def.EndNpcCodename)
	if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_2_14" || !row.Complete {
		t.Fatalf("feather branch completion row %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, "ITEM_QNO_RM_OLDWOMAN_2_02") != 3 || captureItemCount(c, def.CollectItemCodename) != 0 {
		t.Fatal("feather branch did not pay its scrolls for the feathers", c.MissionInventory)
	}
}

/*
================
TestRahidTwoWaitingBranchCountsOnlineMinutes

The waiting branch counts 1800 admitted minutes, gathers no feathers while it
runs, announces _13 on the minute after the count reaches zero (89E050) and
then completes with _15 for no reward.
================
*/
func TestRahidTwoWaitingBranchCountsOnlineMinutes(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidChainFixture(t, "QNO_RM_OLDWOMAN_2")
	if _, err := rt.StartQuest(c, branchToken(def.Codename, 1)); err != nil {
		t.Fatal(err)
	}
	at := activeQuestIndex(c, def.RefID)
	if c.ActiveQuests[at].Branch != 1 || c.ActiveQuests[at].WaitMinutes != 1800 || c.ActiveQuests[at].Progress != packQuestMinutes(1800) {
		t.Fatalf("waiting record %+v", c.ActiveQuests[at])
	}
	if drops := rt.MonsterDrops(c, "MOB_RM_ROCKY", 0, func() (uint32, error) { return 1, nil }); len(drops) != 0 {
		t.Fatal("feathers dropped while waiting", drops)
	}
	if row, _ := npcRow(rt, c, def.Codename, def.EndNpcCodename); row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_2_08" {
		t.Fatalf("waiting not-achieved row %+v", row)
	}
	holdItems(t, rt, c, def.CollectItemCodename, 7)
	for minute := 1; minute <= 1800; minute++ {
		frames := rt.AdvanceMinute(c)
		// 570650 is sent unless the new count is a multiple of ten.
		if (len(frames) == 0) != ((1800-minute)%10 == 0) {
			t.Fatalf("minute %d sent %d frame(s)", minute, len(frames))
		}
		if objectiveMet(c, def, c.ActiveQuests[at]) || hasNotice(frames, "SN_TALK_QNO_RM_OLDWOMAN_2_13") {
			t.Fatalf("waiting branch achieved after %d minutes", minute)
		}
	}
	if c.ActiveQuests[at].WaitMinutes != 0 || c.ActiveQuests[at].Progress != packQuestMinutes(0) {
		t.Fatalf("spent record %+v", c.ActiveQuests[at])
	}
	if !hasNotice(rt.AdvanceMinute(c), "SN_TALK_QNO_RM_OLDWOMAN_2_13") || !objectiveMet(c, def, c.ActiveQuests[at]) {
		t.Fatal("the minute after zero did not announce _13")
	}
	if frames := rt.AdvanceMinute(c); len(frames) != 0 {
		t.Fatal("an achieved wait kept counting", frames)
	}
	row, _ := npcRow(rt, c, def.Codename, def.EndNpcCodename)
	if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_2_15" || !row.Complete {
		t.Fatalf("waiting completion row %+v", row)
	}
	experience := *c.Experience
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if *c.Experience != experience || captureItemCount(c, "ITEM_QNO_RM_OLDWOMAN_2_02") != 0 || !questCompleted(c, def.RefID) {
		t.Fatal("waiting branch paid a reward or kept the quest")
	}
	if captureItemCount(c, def.CollectItemCodename) != 0 {
		t.Fatal("waiting branch kept the gathered feathers")
	}
}

/*
================
TestRahidThreeAskAhmokThenReportToBukhra

Accepting notices _12; Bukhra repeats _04 until Ahmok's two pages and _09
are done, which announce _13; then Bukhra's _11 completes.
================
*/
func TestRahidThreeAskAhmokThenReportToBukhra(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidChainFixture(t, "QNO_RM_OLDWOMAN_3")
	result, err := rt.StartQuest(c, def.Codename)
	if err != nil || !hasNotice(result.Frames, "SN_TALK_QNO_RM_OLDWOMAN_3_12") {
		t.Fatalf("accept notice missing: %v", err)
	}
	if row, _ := npcRow(rt, c, def.Codename, "NPC_RM_VILLAGECHIEF"); row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_3_04" || !row.Informational {
		t.Fatalf("Bukhra before Hotan %+v", row)
	}
	row, _ := npcRow(rt, c, def.Codename, "NPC_KT_MINISTER")
	if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_3_09" || len(row.Pages) != 2 || row.Pages[1].ReplySymbol != "SN_TALK_QNO_RM_OLDWOMAN_3_08" {
		t.Fatalf("Ahmok row %+v", row)
	}
	result, err = rt.AdvanceNpcQuest(c, row.Codename, "NPC_KT_MINISTER")
	if err != nil || !hasNotice(result.Frames, "SN_TALK_QNO_RM_OLDWOMAN_3_13") {
		t.Fatalf("Ahmok's exchange did not announce _13: %v", err)
	}
	row, _ = npcRow(rt, c, def.Codename, "NPC_RM_VILLAGECHIEF")
	if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_3_11" || !row.Complete {
		t.Fatalf("Bukhra after Hotan %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, row.Codename, "NPC_RM_VILLAGECHIEF"); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Bukhra did not complete Rahid 3: %v", err)
	}
}

/*
================
TestRahidFourSlavesAnswerShiphrCompletes

Bukhra only answers _04. The two slaves who never met Rahid speak _05 / _07
once, recorded through their side-talk token, and then answer _06 / _08;
Shiphr's _09 completes.
================
*/
func TestRahidFourSlavesAnswerShiphrCompletes(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidChainFixture(t, "QNO_RM_OLDWOMAN_4")
	result, err := rt.StartQuest(c, def.Codename)
	if err != nil || !hasNotice(result.Frames, "SN_TALK_QNO_RM_OLDWOMAN_4_10") {
		t.Fatalf("accept notice missing: %v", err)
	}
	if row, _ := npcRow(rt, c, def.Codename, "NPC_RM_VILLAGECHIEF"); row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_4_04" || !row.Informational {
		t.Fatalf("Bukhra row %+v", row)
	}
	for npc, lines := range map[string][2]string{
		"NPC_RM_SLAVE1": {"SN_TALK_QNO_RM_OLDWOMAN_4_05", "SN_TALK_QNO_RM_OLDWOMAN_4_06"},
		"NPC_RM_SLAVE3": {"SN_TALK_QNO_RM_OLDWOMAN_4_07", "SN_TALK_QNO_RM_OLDWOMAN_4_08"},
	} {
		row, _ := npcRow(rt, c, def.Codename, npc)
		if row.PromptSymbol != lines[0] || !row.SideTalk || row.Informational {
			t.Fatalf("%s pending row %+v", npc, row)
		}
		if _, err := rt.AdvanceNpcQuest(c, row.Codename, "NPC_RM_SLAVE2"); err == nil {
			t.Fatalf("%s side talk recorded at the wrong NPC", npc)
		}
		if _, err := rt.AdvanceNpcQuest(c, row.Codename, npc); err != nil {
			t.Fatal(err)
		}
		if _, err := rt.AdvanceNpcQuest(c, row.Codename, npc); err == nil {
			t.Fatalf("%s side talk recorded twice", npc)
		}
		if row, _ := npcRow(rt, c, def.Codename, npc); row.PromptSymbol != lines[1] || !row.Informational || row.SideTalk {
			t.Fatalf("%s heard row %+v", npc, row)
		}
	}
	row, _ := npcRow(rt, c, def.Codename, "NPC_RM_SLAVE2")
	if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_4_09" || !row.Complete {
		t.Fatalf("Shiphr row %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, "NPC_RM_SLAVE2"); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Shiphr did not complete Rahid 4: %v", err)
	}
}
