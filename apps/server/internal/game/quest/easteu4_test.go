/*
===========================================================================

easteu4_test.go - QNO_EU_EASTEU_4's story page before the offer

8AB930 is the base talk behind one page: the Sunset Witch asks _01 with
the reply _02, then offers the quest with the 0x130 word.

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
TestSunsetWitchPagesBeforeTheOffer
================
*/
func TestSunsetWitchPagesBeforeTheOffer(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	// The shipped questdata row offers it from level 12.
	level, gold := int64(12), int64(0)
	c := &enterworld.Character{ID: 11, Name: "stablehand", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	offer, offered := npcRow(rt, c, "QNO_EU_EASTEU_4", "NPC_EU_WITCH")
	if !offered || offer.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_4_03" || len(offer.Pages) != 1 ||
		offer.Pages[0] != (OfferPage{PromptSymbol: "SN_TALK_QNO_EU_EASTEU_4_01", ReplySymbol: "SN_TALK_QNO_EU_EASTEU_4_02"}) {
		t.Fatalf("the Sunset Witch's offer %+v (offered %v)", offer, offered)
	}
}
