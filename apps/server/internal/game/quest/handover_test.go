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
