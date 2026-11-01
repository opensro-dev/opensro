/*
===========================================================================

tradestars.go - the transport bag's trade level stars (0x352D)

The v1.150 client keeps one byte of trade level on the local player
(CICPlayer +0x4F6) that only 0x352D [u8] writes
(CPSMission_OnTradeStarCount0x352D 75B8D0); the transport bag's TradeInfo
section shows that many com_star icons (CIFCosBag_SetTradeStarCount
client 6A7DD0, capped at 5). The reference server no longer sends 0x352D: it
publishes the same trade difficulty tier value in the
COS state 0x30CA (bit 4), which the
v1.150 client has no handler for. INFERENCE from that v1.188 publisher:
the v1.150 server sent the cargo tier through 0x352D whenever the cargo
changed, so this port sends it to the owner after every move that loads
or unloads the transport.

===========================================================================
*/

package action

import (
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
		// Native from cargo units; the gold tier under SRO_TRADE_TIERS
		// (tradetiers.go), the same tier the bandits read.
		tier, _ = rt.caravanTier(c)
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
of the move, as the client applies them in that order. The move's door has
returned by now, so the transport and its bag are read under the store's
read lock: tick doors (a cargo drop) and other moves may change them.
================
*/
func (rt *Runtime) withTradeStars(division string, c *enterworld.Character, movement uint8, result OpResult) OpResult {
	if !cargoMove(movement) || !committedMove(result) {
		return result
	}
	// Only a transport has a trade level; a pet's bag moves tell nothing.
	var frame wire.Frame
	summoned := false
	rt.deps.Read(division, func() {
		if summoned = rt.transportSummoned(c); summoned {
			frame = rt.tradeStarFrame(c)
		}
	})
	if summoned {
		result.Frames = append(result.Frames, frame)
	}
	return result
}

/*
================
committedMove

Whether a move's answer committed: its OpItemMoveResponse (which may follow
the references a shop publishes first) leads with 1.
================
*/
func committedMove(result OpResult) bool {
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpItemMoveResponse {
			return len(frame.Payload) != 0 && frame.Payload[0] == 1
		}
	}
	return false
}

/*
================
sceneTradeStars

The trade level a scene going live repeats: only for a summoned transport
with goods aboard (ground_scene.go ReconcileScene). c is the game-ready's
detached snapshot, which no door changes.
================
*/
func (rt *Runtime) sceneTradeStars(c *enterworld.Character) (wire.Frame, bool) {
	if c == nil || !rt.transportSummoned(c) || !vehicleCarriesGoods(rt, c) {
		return wire.Frame{}, false
	}
	return rt.tradeStarFrame(c), true
}
