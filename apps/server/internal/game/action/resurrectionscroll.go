/*
===========================================================================

resurrectionscroll.go - the resurrection scroll: a dead player revives itself

ITEM_MALL_RESURRECTION_60P/100P_SCROLL and four quest rewards share type
3/3/13/6. CGItemExpendable_UseSpecialConsumable (v1.188 49C2B0) sends type
4 = 6 to CGItemExpendable_CheckPlayerReturnCondition (49FF20) with the
item's Param1..3 (RefItemData +0x2A0/+0x2C0/+0x2E0 on the server):

  - a living player is refused with 0x1887 (v1.150 notice 391,
    UIIT_MSG_STRGERR_ONLY_THE_DEAD_CAN_USE_RESURRECT_SCROLL);
  - otherwise the player revives where the corpse lies (4DF290 arg 1, the
    same revival a resurrection skill commits: reviveWhereDead);
  - the EXP refund is Param3 percent of the recorded loss, rounded up;
  - HP and MP are both Param1 when it is set, else Param2 percent of their
    maxima (every published scroll: Param2 100, Param3 60 or 100).

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// errCodeOnlyDeadResurrect is the low byte of 49FF20's 0x1887.
const errCodeOnlyDeadResurrect uint8 = 0x87

// resurrectionScrollRoundUp is 49FFEC's float constant: a fraction above
// it rounds the refund up.
const resurrectionScrollRoundUp = float32(9.99999997e-07)

/*
==================
resurrectionScrollExp

49FF43..49FFFC: |loss| (read before the revival clears it) times
Param3/100, stored as float32 and halved for a murderer (state+0xC is 2);
the product is a float32 truncated by fistp, plus one when the float32
remainder exceeds resurrectionScrollRoundUp. Unlike the skill's refund
(resurrectionExp), the scroll rounds up.
==================
*/
func resurrectionScrollExp(lastLoss int64, percent uint32, murderer bool) int64 {
	loss := int64(int32(lastLoss))
	if loss < 0 {
		loss = -loss
	}
	share := float32(float64(percent) / 100)
	if murderer {
		share = float32(float64(share) * 0.5)
	}
	exp := float32(float64(loss) * float64(share))
	whole := math.Trunc(float64(exp))
	if float32(float64(exp)-whole) > resurrectionScrollRoundUp {
		whole++
	}
	return int64(whole)
}

/*
==================
resurrectionScrollVitals

4A0000..4A0060: Param1 > 0 sets both HP and MP to it; otherwise Param2 > 0
gives each Param2/100 (a float32) of its maximum, truncated.
==================
*/
func resurrectionScrollVitals(amount, percent uint32, maxHP, maxMP int64) (hp, mp int64) {
	if amount > 0 {
		return int64(amount), int64(amount)
	}
	if percent == 0 {
		return 0, 0
	}
	share := float64(float32(float64(percent) / 100))
	return int64(crtFtol(float64(maxHP) * share)), int64(crtFtol(float64(maxMP) * share))
}

/*
==================
itemParamUint

An itemdata param projected into NativeFields, or 0 when absent or not a
whole non-negative 32-bit value.
==================
*/
func itemParamUint(ref *enterworld.ItemRef, name string) uint32 {
	value, ok := ref.NativeFields.Lookup(name)
	if !ok || math.IsNaN(value) || value < 0 || value > math.MaxUint32 || math.Trunc(value) != value {
		return 0
	}
	return uint32(value)
}

/*
==================
useResurrectionScroll

Runs inside HandleItemUse's character Update under the division lock.
On success it consumes the scroll, revives the player and leaves the
post-commit half (revivalFrames) to *after.
==================
*/
func (rt *Runtime) useResurrectionScroll(character *enterworld.Character, use skillItemUse, tail []byte, result *OpResult, after *func()) bool {
	if len(tail) != 0 {
		return false
	}
	if enterworld.CharacterAlive(character) {
		*result = itemUseFailure(errCodeOnlyDeadResurrect)
		return false
	}
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(use.division, character)
	hp, mp := resurrectionScrollVitals(
		itemParamUint(use.ref, "itemParam1_29c"),
		itemParamUint(use.ref, "itemParam2_2a0"),
		maxHP, maxMP,
	)
	offer := resurrectionOffer{
		exp: resurrectionScrollExp(character.LastExpLoss, itemParamUint(use.ref, "itemParam3_2a4"),
			character.PVPState() == 2),
		hp: hp,
		mp: mp,
	}
	remaining := rt.consumeItemUseRow(character, use.row)
	revived := rt.reviveWhereDead(use.division, character, offer, use.nowMs)
	used := []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}
	used = append(used, rt.updateQuestInventory(character)...)
	*after = func() {
		actor, peers := rt.revivalFrames(use.division, character, revived, use.nowMs)
		*result = OpResult{Frames: append(used, actor...), Broadcast: peers}
	}
	return true
}
