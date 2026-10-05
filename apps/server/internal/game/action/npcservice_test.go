/*
===========================================================================

npcservice_test.go - NPC service admission and interaction wire responses

The shop action must preserve the selected service through its capability
gate, native acknowledgement and catalog publication. Special trade has
its own capability and an additional mode byte in the v1.150 response.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestShopServiceAcknowledgementPrecedesCatalog
================
*/
func TestShopServiceAcknowledgementPrecedesCatalog(t *testing.T) {
	for _, special := range []bool{false, true} {
		rt, c := merchantFixture(t)
		rt.Selected.Set(testDivision, c.Name, 17)
		mask := simulation.NpcTalkFlagShop
		if special {
			mask = simulation.NpcTalkFlagSpecialTrade
			rt.NpcRoster[0].TalkFlags |= mask
		}
		frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, mask))
		want := wire.EncodeNpcInteractionAck(mask)
		if special {
			want = append(want, 0)
		}
		if refusal != "" || len(frames) != 2 || frames[0].Opcode != wire.OpNpcInteractionAck ||
			!bytes.Equal(frames[0].Payload, want) || frames[1].Opcode != 11 {
			t.Fatalf("special %v: frames %+v refusal %q", special, frames, refusal)
		}
		if !rt.Selected.FunctionOpen(testDivision, c.Name, 17) {
			t.Fatal("accepted shop has no open function")
		}
	}
}

/*
================
TestOrdinaryMerchantCannotGrantSpecialTrade
================
*/
func TestOrdinaryMerchantCannotGrantSpecialTrade(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagSpecialTrade))
	if refusal == "" || len(frames) != 0 || rt.Selected.FunctionOpen(testDivision, c.Name, 17) {
		t.Fatalf("ordinary shop granted special trade: %+v %q", frames, refusal)
	}
}
