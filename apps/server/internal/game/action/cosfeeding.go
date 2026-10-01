/*
===========================================================================

cosfeeding.go - atomic pet-food consumption and authored satiety recovery

The owner commits the food debit and companion recovery together. Neither a
client-supplied target nor an untyped item parameter grants feeding authority.

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	cosFeedingRefusalThreshold        = 9900
	cosFeedingFullError        uint8  = 0xb3
	cosPetUpdateOpcode         uint16 = 0x3508
	cosSatietyUpdateKind       uint8  = 4
)

/*
================
petFeedUse

All fields come from the authenticated item-use transaction, after row and
reference validation. The pet target remains an untrusted native wire tail.
================
*/
type petFeedUse struct {
	character *enterworld.Character
	ref       *enterworld.ItemRef
	row       int
	request   wire.ItemUseRequest
	tail      []byte
}

/*
================
applyPetFeed

49D240/49D77C: only the owner's live attack pet accepts HGP food below 99%.
The later server reads Param1 at +2A0 (v1.150 +29C). It is a percentage
of 10000, and recovery saturates at 10000. The native
food arm does not start the HP/MP item reuse timer.
================
*/
func (rt *Runtime) applyPetFeed(use petFeedUse, result *OpResult) bool {
	c, ref, row, request, tail := use.character, use.ref, use.row, use.request, use.tail
	gid, ok := readCosGID(tail)
	if !ok || !rt.livePet(c, gid) {
		*result = itemUseFailure(wire.ErrCodeCosRefused)
		return false
	}
	petRef, valid := rt.cosReference(c.CompanionByGID(gid))
	if !valid || petRef.TidWord>>11 != 3 {
		*result = itemUseFailure(wire.ErrCodeCosRefused)
		return false
	}
	pet := c.CompanionByGID(gid)
	if pet.Satiety >= cosFeedingRefusalThreshold {
		*result = itemUseFailure(cosFeedingFullError)
		return false
	}
	percent, present := ref.NativeFields.Lookup("itemParam1_29c")
	if !present || math.IsNaN(percent) || math.IsInf(percent, 0) || math.Trunc(percent) != percent || percent < 1 || percent >= 100 {
		return false
	}
	pet.Satiety = uint16(min(companion.MaximumSatiety, int(pet.Satiety)+int(percent)*companion.MaximumSatiety/100))
	remaining := rt.consumeItemUseRow(c, row)
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
		// v1.150 77A570 is the 3508 owner record, not the later server's 30C9.
		{Opcode: cosPetUpdateOpcode, Payload: wire.NewWriter(7).U32(gid).U8(cosSatietyUpdateKind).U16(pet.Satiety).Payload()},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}
