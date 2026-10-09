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

/*
================
TestParallelDeliveriesHandOverInEitherOrder

QNO_CH_POTION_4 carries Doji's medicine and Bori's book together. Each
latches its own mission at its own NPC (91CEB0's per-mission bit), the
other's +0x108 line reminds the player of what is left, the journal moves
to the remaining NPC, and Yangyun pays once both are handed over.
================
*/
func TestParallelDeliveriesHandOverInEitherOrder(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_CH_POTION_4")
	if def.Objective != ObjectiveParallel || len(def.Objectives) != 2 {
		t.Fatalf("POTION_4 objective %d with %d missions", def.Objective, len(def.Objectives))
	}
	c := deliveryCharacter(t, rt, "QNO_CH_POTION_2")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	const medicine, book = "ITEM_QNO_CH_POTION_4_01", "ITEM_QNO_CH_POTION_4_02"
	if captureItemCount(c, medicine) != 1 || captureItemCount(c, book) != 1 {
		t.Fatal("acceptance did not grant both deliveries")
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("Yangyun paid before either hand-over")
	}
	// The book first, at Chau.
	row, ok := npcRow(rt, c, def.Codename, "NPC_CH_FERRY")
	if !ok || row.Codename != handOverToken(def.Codename) || row.PromptSymbol != "SN_TALK_QNO_CH_POTION_4_08" {
		t.Fatalf("Chau's hand-over row %+v", row)
	}
	out, err := rt.AdvanceNpcQuest(c, row.Codename, "NPC_CH_FERRY")
	if err != nil {
		t.Fatalf("hand the book over: %v", err)
	}
	if captureItemCount(c, book) != 0 || captureItemCount(c, medicine) != 1 {
		t.Fatal("Chau did not take only the book")
	}
	if !hasNotice(out.Frames, "SN_TALK_QNO_CH_POTION_4_13") || hasNotice(out.Frames, "SN_TALK_QNO_CH_POTION_4_11") {
		t.Fatal("the book's hand-over did not remind of the medicine, or announced completion early")
	}
	if target := questTarget(c, def, c.ActiveQuests[0]); target.Codename != "NPC_CH_FERRY2" {
		t.Fatalf("journal after the book points at %s, want Doji", target.Codename)
	}
	// Even holding a book again, Chau's mission stays latched.
	holdItems(t, rt, c, book, 1)
	if _, err := rt.AdvanceNpcQuest(c, row.Codename, "NPC_CH_FERRY"); err == nil {
		t.Fatal("Chau took the book twice")
	}
	if _, offered := npcRow(rt, c, def.Codename, "NPC_CH_FERRY"); offered {
		if again, _ := npcRow(rt, c, def.Codename, "NPC_CH_FERRY"); again.Codename == handOverToken(def.Codename) {
			t.Fatal("Chau offers a second hand-over")
		}
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("Yangyun paid with the medicine still undelivered")
	}
	out, err = rt.AdvanceNpcQuest(c, handOverToken(def.Codename), "NPC_CH_FERRY2")
	if err != nil {
		t.Fatalf("hand the medicine over: %v", err)
	}
	if !hasNotice(out.Frames, "SN_TALK_QNO_CH_POTION_4_11") {
		t.Fatal("the last hand-over did not send the achieved-now line")
	}
	if target := questTarget(c, def, c.ActiveQuests[0]); target.Codename != def.EndNpcCodename || target.State != markerStateReport {
		t.Fatalf("journal after both points at %+v, want Yangyun to report", target)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Yangyun did not pay: %v", err)
	}
}

/*
================
TestParallelExchangesReturnAtTheReport

QNO_RM_SLAVE1_3's two plans each come back as an exchange at their NPC
after one [NEXT] page; Jabr takes both back with the reward, and an
abandoned quest leaves no plan behind.
================
*/
func TestParallelExchangesReturnAtTheReport(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_RM_SLAVE1_3")
	newCharacter := func() *enterworld.Character {
		c := deliveryCharacter(t, rt, "QNO_RM_SLAVE1_2")
		*c.Level = int64(max(def.Level, 80))
		c.ModelCodename = "CHAR_EU_MAN_NOBLE"
		if def.CountryByte == 0 {
			c.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
		}
		if _, err := rt.StartQuest(c, def.Codename); err != nil {
			t.Fatalf("accept: %v", err)
		}
		return c
	}
	c := newCharacter()
	for _, npc := range []string{"NPC_RM_SLAVE2", "NPC_RM_SLAVE3"} {
		row, ok := npcRow(rt, c, def.Codename, npc)
		if !ok || len(row.Pages) != 1 || row.Pages[0].ReplySymbol != "SN_TALK_COMMON_NEXT" {
			t.Fatalf("%s hand-over row %+v", npc, row)
		}
		if _, err := rt.AdvanceNpcQuest(c, row.Codename, npc); err != nil {
			t.Fatalf("hand over at %s: %v", npc, err)
		}
	}
	if captureItemCount(c, "ITEM_QNO_RM_SLAVE1_3_02") != 1 || captureItemCount(c, "ITEM_QNO_RM_SLAVE1_3_03") != 1 {
		t.Fatal("the hand-overs did not give both plans back")
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatalf("report to Jabr: %v", err)
	}
	if captureItemCount(c, "ITEM_QNO_RM_SLAVE1_3_02") != 0 || captureItemCount(c, "ITEM_QNO_RM_SLAVE1_3_03") != 0 {
		t.Fatal("Jabr did not take the plans back")
	}
	abandoned := newCharacter()
	if _, err := rt.AdvanceNpcQuest(abandoned, handOverToken(def.Codename), "NPC_RM_SLAVE2"); err != nil {
		t.Fatalf("hand over at SLAVE2: %v", err)
	}
	held := map[string]uint32{}
	for _, item := range deliveryCleanup(abandoned, def) {
		held[item.Codename] = item.Count
	}
	if held["ITEM_QNO_RM_SLAVE1_3_04"] != 1 || held["ITEM_QNO_RM_SLAVE1_3_02"] != 1 || len(held) != 2 {
		t.Fatalf("abandonment would remove %v, want the undelivered plan and the exchange", held)
	}
}

/*
================
TestReceiveOnlyHandOverAmongGathers

QNO_CA_TREASURE_4 gathers Ong's tear and tail and receives Samarkand's
Water at the potion shop: a hand-over that takes nothing and gives the
exchange. Tricia pays once all three missions stand, and takes the water.
================
*/
func TestReceiveOnlyHandOverAmongGathers(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_CA_TREASURE_4")
	if def.Objective != ObjectiveParallel || len(def.Objectives) != 3 {
		t.Fatalf("TREASURE_4 objective %d with %d missions", def.Objective, len(def.Objectives))
	}
	c := deliveryCharacter(t, rt, "QNO_CA_TREASURE_3")
	*c.Level = int64(max(def.Level, 34))
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	if def.CountryByte == 0 {
		c.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
	}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	const water = "ITEM_QNO_CA_TREASURE_4_03"
	// The gathers alone never pay: the receive-only mission is met at once
	// for its hand-over, but completes only on the hand-over's latch.
	for _, m := range def.Objectives[:2] {
		holdItems(t, rt, c, m.CollectItemCodename, m.CollectCount)
	}
	rt.InventoryUpdater()(c)
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("Tricia paid before the water was received")
	}
	row, ok := npcRow(rt, c, def.Codename, "NPC_CA_POTION")
	if !ok || row.Codename != handOverToken(def.Codename) || row.PromptSymbol != "SN_TALK_QNO_CA_TREASURE_4_07" {
		t.Fatalf("the potion shop's row %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, row.Codename, "NPC_CA_POTION"); err != nil || captureItemCount(c, water) != 1 {
		t.Fatalf("receiving the water: %v (holding %d)", err, captureItemCount(c, water))
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Tricia did not pay: %v", err)
	}
	if captureItemCount(c, water) != 0 {
		t.Fatal("Tricia did not take the water back")
	}
}
