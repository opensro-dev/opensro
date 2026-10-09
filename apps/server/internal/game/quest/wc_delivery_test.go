/*
===========================================================================

wc_delivery_test.go - the 8E6440 deliveries: Bori's birthday gifts and paddles

QNO_WC_POTION_3 hands over medicine and a letter that stay in the bag
(+0x111 cleared) and sends _16 once it completes; QNO_WC_POTION_4 takes them
at acceptance, on _01's NEXT, before it grants the wrapped gift.
QNO_WC_SMITH_3 is an ordinary fifty-paddle delivery with its own lines.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
deliveryCharacter

A level 40 character who finished the named predecessor.
================
*/
func deliveryCharacter(t *testing.T, rt *Runtime, predecessor string) *enterworld.Character {
	t.Helper()
	parent, ok := rt.Defs.ByCodename(predecessor)
	if !ok {
		t.Fatalf("%s is not loaded", predecessor)
	}
	level, gold := int64(40), int64(0)
	c := &enterworld.Character{ID: 9, Name: "courier", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold}
	c.CompletedQuestIds = []uint32{parent.RefID}
	return c
}

/*
================
TestBirthdayGiftsStayForTheSecondDelivery
================
*/
func TestBirthdayGiftsStayForTheSecondDelivery(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_POTION_2")
	const medicine, letter, gift = "ITEM_QNO_WC_POTION_3_01", "ITEM_QNO_WC_POTION_3_02", "ITEM_QNO_WC_POTION_3_03"

	offer, offered := npcRow(rt, c, "QNO_WC_POTION_3", "NPC_WC_POTION")
	if !offered || offer.PromptSymbol != "SN_TALK_QNO_WC_POTION_3_02" || len(offer.Pages) != 1 ||
		offer.Pages[0].ReplySymbol != "SN_TALK_COMMON_NEXT" || offer.AcceptRowSymbol != "" {
		t.Fatalf("Bori's offer %+v (offered %v)", offer, offered)
	}
	if _, err := rt.StartQuest(c, "QNO_WC_POTION_3"); err != nil {
		t.Fatalf("accept POTION_3: %v", err)
	}
	if captureItemCount(c, medicine) != 1 || captureItemCount(c, letter) != 1 {
		t.Fatal("acceptance did not hand over the medicine and the letter")
	}
	held := c.MissionInventory
	c.MissionInventory = nil
	if missing, _ := npcRow(rt, c, "QNO_WC_POTION_3", "NPC_CH_ACCESSORY"); !missing.Informational ||
		missing.PromptSymbol != "SN_TALK_QNO_WC_ACCESSORY_3_10" {
		t.Fatalf("Jinjin without the gifts: %+v, want the mission's +0xC4 line", missing)
	}
	c.MissionInventory = held
	if row, _ := npcRow(rt, c, "QNO_WC_POTION_3", "NPC_WC_POTION"); !row.Informational || row.PromptSymbol != "SN_TALK_QNO_WC_POTION_3_05" {
		t.Fatalf("Bori while the delivery runs: %+v", row)
	}
	out, err := rt.AdvanceNpcQuest(c, "QNO_WC_POTION_3", "NPC_CH_ACCESSORY")
	if err != nil {
		t.Fatalf("hand over to Jinjin: %v", err)
	}
	if !questCompleted(c, mustQuest(t, rt, "QNO_WC_POTION_3").RefID) || !hasNotice(out.Frames[:1], "SN_TALK_QNO_WC_POTION_3_16") {
		t.Fatal("the hand-over did not complete with _16 sent first")
	}
	if captureItemCount(c, medicine) != 1 || captureItemCount(c, letter) != 1 {
		t.Fatal("the hand-over took the gifts it keeps (+0x111 cleared)")
	}

	offer, offered = npcRow(rt, c, "QNO_WC_POTION_4", "NPC_CH_ACCESSORY")
	if !offered || offer.PromptSymbol != "SN_TALK_QNO_WC_POTION_4_01" || offer.AcceptRowSymbol != "SN_TALK_COMMON_NEXT" {
		t.Fatalf("Jinjin's offer %+v (offered %v)", offer, offered)
	}
	if _, err := rt.StartQuest(c, "QNO_WC_POTION_4"); err != nil {
		t.Fatalf("accept POTION_4: %v", err)
	}
	if captureItemCount(c, medicine) != 0 || captureItemCount(c, letter) != 0 || captureItemCount(c, gift) != 1 {
		t.Fatalf("acceptance left medicine %d letter %d gift %d", captureItemCount(c, medicine), captureItemCount(c, letter), captureItemCount(c, gift))
	}
	turnIn, ok := npcRow(rt, c, "QNO_WC_POTION_4", "NPC_WC_SPECIAL")
	if !ok || !turnIn.Complete || len(turnIn.Pages) != 1 || turnIn.PromptSymbol != "SN_TALK_QNO_WC_POTION_4_06" {
		t.Fatalf("Asa's turn-in %+v", turnIn)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QNO_WC_POTION_4", "NPC_WC_SPECIAL"); err != nil {
		t.Fatalf("hand over to Asa: %v", err)
	}
	if captureItemCount(c, gift) != 0 || captureItemCount(c, "ITEM_QNO_WC_POTION_4_01") != 2 {
		t.Fatal("Asa did not take the gift and pay two rewards")
	}
}

/*
================
TestSecondGiftAcceptsWithoutTheFirst

897680 only raises a minidump when the medicine or letter is gone; the
wrapped gift is still granted and the quest accepted.
================
*/
func TestSecondGiftAcceptsWithoutTheFirst(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_POTION_3")
	if _, err := rt.StartQuest(c, "QNO_WC_POTION_4"); err != nil {
		t.Fatalf("accept without the first gifts: %v", err)
	}
	if captureItemCount(c, "ITEM_QNO_WC_POTION_3_03") != 1 {
		t.Fatal("the wrapped gift was not granted")
	}
}

/*
================
TestPaddleDeliveryAsksAndAnswers

SMITH_3 asks _01 with yes/no, hands over fifty paddles, and Salmai takes
all fifty; Agol's _04 answers while they travel.
================
*/
func TestPaddleDeliveryAsksAndAnswers(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	c := deliveryCharacter(t, rt, "QNO_WC_SMITH_2")
	const paddle = "ITEM_QNO_WC_SMITH_3_01"
	offer, offered := npcRow(rt, c, "QNO_WC_SMITH_3", "NPC_WC_SMITH")
	if !offered || offer.PromptSymbol != "SN_TALK_QNO_WC_SMITH_3_01" || len(offer.Pages) != 0 ||
		offer.DenyResponseSymbol != "SN_TALK_QNO_WC_SMITH_3_03" || offer.AcceptRowSymbol != "" {
		t.Fatalf("Agol's offer %+v (offered %v)", offer, offered)
	}
	if _, err := rt.StartQuest(c, "QNO_WC_SMITH_3"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if captureItemCount(c, paddle) != 50 {
		t.Fatalf("holding %d paddles, want 50", captureItemCount(c, paddle))
	}
	if row, _ := npcRow(rt, c, "QNO_WC_SMITH_3", "NPC_WC_SMITH"); row.PromptSymbol != "SN_TALK_QNO_WC_SMITH_3_04" {
		t.Fatalf("Agol while the paddles travel: %+v", row)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QNO_WC_SMITH_3", "NPC_WC_FERRY3"); err != nil {
		t.Fatalf("hand over to Salmai: %v", err)
	}
	if captureItemCount(c, paddle) != 0 || !questCompleted(c, mustQuest(t, rt, "QNO_WC_SMITH_3").RefID) {
		t.Fatal("Salmai did not take the paddles and complete the quest")
	}
}

/*
================
mustQuest
================
*/
func mustQuest(t *testing.T, rt *Runtime, code string) *Definition {
	t.Helper()
	def, ok := rt.Defs.ByCodename(code)
	if !ok {
		t.Fatalf("%s is not loaded", code)
	}
	return def
}
