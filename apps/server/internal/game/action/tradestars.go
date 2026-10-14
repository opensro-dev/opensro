/*
===========================================================================

tradestars.go - the transport bag's trade level stars (0x352D)

The v1.150 client keeps one byte of trade level on the local player
(CICPlayer +0x4F6) that only 0x352D [u8] writes
(CPSMission_OnTradeStarCount0x352D 75B8D0); the transport bag's TradeInfo
section shows that many com_star icons (CIFCosBag_SetTradeStarCount
6A7DD0, capped at 5). v1.188 SR_GameServer no longer sends 0x352D: it
publishes the same Caravan_GetTradeDifficultyTier (60C330) value in the
COS state 0x30CA (bit 4, CGObjCOS_PublishState30CA 4D1EA0), which the
v1.150 client has no handler for. INFERENCE from that v1.188 publisher:
the v1.150 server sent the cargo tier through 0x352D whenever the cargo
changed, so this port sends it to the owner after every move that loads
or unloads the transport.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
tradeStarFrame

0x352D [tier]: the summoned transport's cargo difficulty tier, 0 with no
transport or no goods.
================
*/
func (rt *Runtime) tradeStarFrame(c *enterworld.Character) wire.Frame {
	tier := uint8(0)
	if c != nil && vehicleCarriesGoods(rt, c) {
		tier = caravan.DifficultyTier(rt.caravanCargoValue(c))
	}
	return wire.Frame{Opcode: wire.OpTradeStarCount, Payload: []byte{tier}}
}

/*
================
cargoMove

The item moves that load or unload a transport's cargo.
================
*/
func cargoMove(movement uint8) bool {
	switch movement {
	case wire.MoveTypeCosPickup, wire.MoveTypeCosDrop, wire.MoveTypeCosToPlayer, wire.MoveTypePlayerToCos,
		wire.MoveTypeCosShopBuy, wire.MoveTypeCosShopSell:
		return true
	}
	return false
}

/*
================
withTradeStars

A committed cargo move tells the owner its new trade level after every frame
of the move, as the client applies them in that order.
================
*/
func (rt *Runtime) withTradeStars(c *enterworld.Character, movement uint8, result OpResult) OpResult {
	// Only a transport has a trade level; a pet's bag moves tell nothing.
	if !cargoMove(movement) || !rt.transportSummoned(c) {
		return result
	}
	// A shop's answer may follow the references it publishes first.
	for _, frame := range result.Frames {
		if frame.Opcode != wire.OpItemMoveResponse {
			continue
		}
		if len(frame.Payload) != 0 && frame.Payload[0] == 1 {
			result.Frames = append(result.Frames, rt.tradeStarFrame(c))
		}
		return result
	}
	return result
}
