/*
===========================================================================

handover_test.go - two-leg deliveries: hand over, then report

A deliver mission with +0x110 clear (the 872040 default) only latches at
its NPC: 91CA00 takes the items, gives the exchange back, and the quest
then pays at its start NPC, or back at the hand-over NPC. QNO_WC_ACCESSORY_3
takes firecrackers to Jinjin and her receipt back to Yeosun.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	firecrackers = "ITEM_QNO_WC_ACCESSORY_3_01"
	receipt      = "ITEM_QNO_WC_ACCESSORY_3_02"
	jinjin       = "NPC_CH_ACCESSORY"
	yeosun       = "NPC_WC_ACCESSORY"
)

/*
================
TestFirecrackersHandOverThenReportToYeosun
================
*/
func TestFirecrackersHandOverThenReportToYeosun(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_ACCESSORY_2")
	def := mustQuest(t, rt, "QNO_WC_ACCESSORY_3")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if target := questTarget(c, def, c.ActiveQuests[0]); target.Codename != jinjin || target.State != markerStateReport {
		t.Fatalf("journal sends the player to %+v, want Jinjin", target)
	}
	if row, _ := npcRow(rt, c, def.Codename, yeosun); !row.Informational || row.PromptSymbol != "SN_TALK_QNO_WC_ACCESSORY_3_04" {
		t.Fatalf("Yeosun before the hand-over: %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, yeosun); err == nil {
		t.Fatal("Yeosun paid before the hand-over")
	}

	row, ok := npcRow(rt, c, def.Codename, jinjin)
	if !ok || !row.Complete || row.Codename != handOverToken(def.Codename) || row.PromptSymbol != "SN_TALK_QNO_WC_ACCESSORY_3_06" {
		t.Fatalf("Jinjin's hand-over row %+v", row)
	}
	out, err := rt.AdvanceNpcQuest(c, row.Codename, jinjin)
	if err != nil {
		t.Fatalf("hand over: %v", err)
	}
	if captureItemCount(c, firecrackers) != 0 || captureItemCount(c, receipt) != 1 {
		t.Fatalf("after the hand-over: firecrackers %d receipt %d", captureItemCount(c, firecrackers), captureItemCount(c, receipt))
	}
	if !hasNotice(out.Frames, "SN_TALK_QNO_WC_ACCESSORY_3_09") || !handedOver(c.ActiveQuests[0]) || c.ActiveQuests[0].Contents[0].Kind != 0 {
		t.Fatal("the hand-over did not latch the mission with the achieved-now banner")
	}
	if questCompleted(c, def.RefID) {
		t.Fatal("the hand-over paid the reward")
	}
	if _, err := rt.AdvanceNpcQuest(c, row.Codename, jinjin); err == nil {
		t.Fatal("a second hand-over was accepted")
	}
	if target := questTarget(c, def, c.ActiveQuests[0]); target.Codename != yeosun || target.State != markerStateReport {
		t.Fatalf("after the hand-over the journal sends the player to %+v, want Yeosun", target)
	}

	report, ok := npcRow(rt, c, def.Codename, yeosun)
	if !ok || !report.Complete || report.PromptSymbol != "SN_TALK_QNO_WC_ACCESSORY_3_07" {
		t.Fatalf("Yeosun's report row %+v", report)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, yeosun); err != nil {
		t.Fatalf("report to Yeosun: %v", err)
	}
	if !questCompleted(c, def.RefID) || captureItemCount(c, receipt) != 0 || captureItemCount(c, "ITEM_QNO_WC_ACCESSORY_3_03") != 1 {
		t.Fatal("Yeosun did not take the receipt and pay")
	}
}

/*
================
TestHandOverNpcPaysOnceHandedOver

CBasicQuest_vf154 admits both NPCs of the quest's table: back at Jinjin,
the handed-over quest pays as it would at Yeosun.
================
*/
func TestHandOverNpcPaysOnceHandedOver(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_ACCESSORY_2")
	def := mustQuest(t, rt, "QNO_WC_ACCESSORY_3")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := rt.AdvanceNpcQuest(c, handOverToken(def.Codename), jinjin); err != nil {
		t.Fatalf("hand over: %v", err)
	}
	if row, ok := npcRow(rt, c, def.Codename, jinjin); !ok || !row.Complete || row.Codename != def.Codename {
		t.Fatalf("Jinjin after the hand-over: %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, jinjin); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Jinjin did not pay the handed-over quest: %v", err)
	}
}

/*
================
TestAbandonedHandOverLeavesNoExchange

Abandonment removes the exchange with the delivered items.
================
*/
func TestAbandonedHandOverLeavesNoExchange(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_ACCESSORY_2")
	def := mustQuest(t, rt, "QNO_WC_ACCESSORY_3")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := rt.AdvanceNpcQuest(c, handOverToken(def.Codename), jinjin); err != nil {
		t.Fatalf("hand over: %v", err)
	}
	cleanup := deliveryCleanup(c, def)
	if len(cleanup) != 1 || cleanup[0].Codename != receipt || cleanup[0].Count != 1 {
		t.Fatalf("abandonment would remove %+v, want the receipt", cleanup)
	}
}

/*
================
TestHandOverRefusesARecordWithoutItsNode

A record without exactly one delivery node is refused under the write
lock, never indexed, and leaves the bag as it was.
================
*/
func TestHandOverRefusesARecordWithoutItsNode(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_ACCESSORY_2")
	def := mustQuest(t, rt, "QNO_WC_ACCESSORY_3")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	c.ActiveQuests[0].Contents = nil
	if _, err := rt.AdvanceNpcQuest(c, handOverToken(def.Codename), jinjin); err == nil {
		t.Fatal("a record without its delivery node was handed over")
	}
	if captureItemCount(c, firecrackers) != 1 || captureItemCount(c, receipt) != 0 {
		t.Fatal("the refused hand-over changed the bag")
	}
}

/*
================
TestHandOverPagesPrecedeTheHandOver

91CA00 pages a delivery's hand-over (+0xBF): QNO_CA_THIEF_4's NPC shows
three pages before the hand-over line _12, and QNO_EU_EASTEU_8's smith one
page [NEXT] before _07, instead of handing over on the first page.
================
*/
func TestHandOverPagesPrecedeTheHandOver(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	thief := mustQuest(t, rt, "QNO_CA_THIEF_4")
	if len(thief.TalkPages) != 3 || thief.TalkPages[0].PromptSymbol != "SN_TALK_QNO_CA_THIEF_4_06" ||
		thief.CompletePromptSymbol != "SN_TALK_QNO_CA_THIEF_4_12" {
		t.Fatalf("THIEF_4 pages %+v, line %s", thief.TalkPages, thief.CompletePromptSymbol)
	}
	witch := mustQuest(t, rt, "QNO_EU_EASTEU_8")
	if len(witch.HandOverPages) != 1 || witch.HandOverPages[0].ReplySymbol != "SN_TALK_COMMON_NEXT" ||
		witch.HandOverSymbol != "SN_TALK_QNO_EU_EASTEU_8_07" || len(witch.TalkPages) != 0 {
		t.Fatalf("EASTEU_8 hand-over pages %+v, line %s, report pages %+v", witch.HandOverPages, witch.HandOverSymbol, witch.TalkPages)
	}
	level, gold := int64(60), int64(0)
	c := &enterworld.Character{ID: 12, Name: "smithrunner", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	c.CompletedQuestIds = []uint32{mustQuest(t, rt, "QNO_EU_EASTEU_7").RefID}
	if _, err := rt.StartQuest(c, witch.Codename); err != nil {
		t.Fatalf("accept EASTEU_8: %v", err)
	}
	row, ok := npcRow(rt, c, witch.Codename, witch.HandOverNpcCodename)
	if !ok || row.Codename != handOverToken(witch.Codename) || len(row.Pages) != 1 || row.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_8_07" {
		t.Fatalf("the smith's hand-over row %+v", row)
	}
}
