/*
===========================================================================

cossell.go - selling a standing transport back at the stable keeper

At a stable keeper, 0x70C7 [u32 npc][u8 kind] kind 3 sells the player's
active vehicle back for its characterdata SellPrice (column 31), held
to 1..10,000,000 and paid as gold; the vehicle record is let go with
its row, like the Clean command's release but paying instead of
dropping cargo. The reference server's order:

  - the named NPC fails its stable service or hit range: 3;
  - no active vehicle, or a kind other than 3: 5;
  - the row cannot be sold (column 17 CanSell 0 - every ride and pet):
    0x440A, UIIT_MSG_COSERR_CANT_SELL_REQUESTED_COS;
  - goods aboard the vehicle: 0x440B,
    UIIT_MSG_COSERR_CANT_SELL_COS_HAVE_ITEMS;
  - the player rides it: 0x4409, UIIT_MSG_COSERR_CANT_SELL_RIDING_COS.

The v1.150 client reads the answer as [1][kind] or [2][code] in
notification category 12, so the word errors travel as their low byte
(cosCancelResult's grammar). Codes 3 and 5 name no category-12 text
and stay silent there.

The native handler first demands an interaction state this port does
not model; the selected-NPC + service + range admission every NPC lane
here uses stands in for it. The stable keeper's client row and its
confirmation box are the client pass's - this lane answers the wire
request whenever it comes.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opCosSellRequest  uint16 = 0x70c7
	opCosSellResponse uint16 = 0xb0c7

	// cosSellKind is the only kind the stable keeper's sale answers.
	cosSellKind uint8 = 3

	cosSellOK            uint8 = 1
	cosSellFailed        uint8 = 2
	cosSellErrNpc        uint8 = 3
	cosSellErrNoVehicle  uint8 = 5
	cosSellErrRiding     uint8 = 0x09
	cosSellErrNotForSale uint8 = 0x0a
	cosSellErrHasGoods   uint8 = 0x0b

	// The paid price is the row's SellPrice held into this band.
	cosSellPriceMin int64 = 1
	cosSellPriceMax int64 = 10_000_000
)

/*
================
cosSellResult

The success answer carries the kind back; a refusal is the [2][code]
pair of the COS lanes.
================
*/
func cosSellResult(code uint8) OpResult {
	payload := []byte{cosSellOK, cosSellKind}
	if code != cosSellOK {
		payload = []byte{2, code}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opCosSellResponse, Payload: payload}}}
}

/*
================
HandleCosSell

One store commit pays the price and lets the vehicle go; observers see
the despawn, the owner the gold, the removal notice and the answer.
================
*/
func (rt *Runtime) HandleCosSell(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	npcGID, gidErr := r.U32()
	kind, kindErr := r.U8()
	if c == nil || gidErr != nil || kindErr != nil || r.Done() != nil {
		return cosSellResult(cosSellErrNoVehicle)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != npcGID {
		return cosSellResult(cosSellErrNpc)
	}
	npc, live := rt.npcForCurrentViewer(division, c, npcGID)
	if !live || !npc.Services.Has(simulation.NpcServiceStable) {
		return cosSellResult(cosSellErrNpc)
	}
	if !rt.npcWithinHitRange(division, c, npc) {
		return cosSellResult(cosSellErrNpc)
	}
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || snapshot.DeletePending {
		return cosSellResult(cosSellErrNoVehicle)
	}
	vehicle := snapshot.ActiveCOS
	if vehicle == nil || !vehicle.Summoned || kind != cosSellKind {
		return cosSellResult(cosSellErrNoVehicle)
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return cosSellResult(cosSellErrNoVehicle)
	}
	ref, found := refs.CharacterRefByCodename(vehicle.Codename)
	if !found || ref == nil || ref.RefObjID != vehicle.RefObjID {
		return cosSellResult(cosSellErrNoVehicle)
	}
	if !ref.CanSell {
		return cosSellResult(cosSellErrNotForSale)
	}
	if vehicle.Container != nil && len(vehicle.Container.Rows) > 0 {
		return cosSellResult(cosSellErrHasGoods)
	}
	if vehicle.Mounted {
		return cosSellResult(cosSellErrRiding)
	}
	price := ref.SellPrice
	if price < cosSellPriceMin {
		price = cosSellPriceMin
	}
	if price > cosSellPriceMax {
		price = cosSellPriceMax
	}
	gid := vehicle.GID
	if !rt.deps.Update(c, "cos-sell", func() bool {
		live := c.ActiveCOS
		if c.DeletePending || live == nil || !live.Summoned || live.GID != gid || live.Mounted ||
			(live.Container != nil && len(live.Container.Rows) > 0) {
			return false
		}
		gold := int64(goldOf(c)) + price
		c.Gold = &gold
		c.ActiveCOS = nil
		return true
	}) {
		return cosSellResult(cosSellFailed)
	}
	frames := []wire.Frame{goldFrame(c)}
	frames = append(frames, rt.retireCosRuntime(division, c, gid)...)
	despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()}
	frames = append(frames, cosRemovalNotice(gid), despawn)
	result := cosSellResult(cosSellOK)
	return OpResult{Frames: append(frames, result.Frames...), Broadcast: []wire.Frame{despawn}}
}
