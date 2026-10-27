/*
===========================================================================

gacha.go - Magic Pop machine interaction, ticket rolls, and Gori prize exchange

Owns 0x7338 machine opening, 0x7053 ticket roll to win/loss vouchers, and
0x71BE voucher redemption through Gori (NPC_CH_GACHA_OPERATOR), including the
native equipment property roll (+1..+5 OptLevel, 5-bit white variance
+ durability, and blue magic options).

===========================================================================
*/

package action

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const gachaRollDenominator uint32 = 10000

// EnvGachaPoolV2 turns the port-only Magic Pop prize pool on (owner,
// 2026-10-20; not native). Unset is on; "off" restores the retail
// gachaitemset.txt pool byte-for-byte.
const EnvGachaPoolV2 = "SRO_GACHA_POOL_V2"

/*
================
GachaPoolV2FromEnv
================
*/
func GachaPoolV2FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGachaPoolV2))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// Native Magic Pop refusal codes, answered [2][u8 code] on the request's
// reply opcode (0xB053 for the roll, 0xB1BE for the exchange). The reference
// server's dispatcher looks the gid up first (no such object: 0; wrong plane
// or non-adjacent sector: 3; beyond the class hit range: 4); its handlers
// then refuse in this order: no gacha service or set 0x1C0F, slot at or
// beyond the bag limit 0x1806, empty slot 0x1809, item not a ticket 0x1806,
// no win entry 2, mutation failure 5. A gid naming an object of another kind
// also reads as no such object here: the roster holds NPCs only. The native
// expired-ticket refusal 0x183D has no port trigger: bag rows carry no
// period state.
const (
	gachaRefusalNoObject   uint16 = 0
	gachaRefusalNoEntry    uint16 = 2
	gachaRefusalOutOfScope uint16 = 3
	gachaRefusalTooFar     uint16 = 4
	gachaRefusalFailed     uint16 = 5
	gachaRefusalBadSlot    uint16 = 0x1806
	gachaRefusalEmptySlot  uint16 = 0x1809
	gachaRefusalNoService  uint16 = 0x1C0F
)

/*
================
gachaRefusal

The one frame a refused roll or exchange answers with. A refusal that sent
nothing left the client in its waiting phase for good: it leaves that phase
only on the reply opcode.
================
*/
func gachaRefusal(opcode uint16, code uint16) []wire.Frame {
	return []wire.Frame{{Opcode: opcode, Payload: gacha.EncodeRefusal(code)}}
}

/*
================
gachaNpcAdmission

The native admission shared by the roll and the exchange: the gid names a
live object (else code 0), in the viewer's plane and adjacent sectors (3),
inside the NPC class hit range (4). The caller then tests its own service
mark and answers 0x1C0F. There is no selected-object test: closing the NPC
talk window releases the selection (0x74B3) while the Magic Pang window
stays open, and the reference server still answers the roll. The reason is
empty only when the NPC is admitted; the code is meaningful only beside a
reason (an admitted NPC and a missing object both carry 0).
================
*/
func (rt *Runtime) gachaNpcAdmission(
	divisionID string,
	character *enterworld.Character,
	gid uint32,
) (simulation.NpcDef, uint16, string) {
	if !rt.NpcSpawn.Enabled {
		return simulation.NpcDef{}, gachaRefusalNoObject, "the NPC roster is not live"
	}
	npc, ok := rt.npcForGID(gid)
	if !ok {
		return simulation.NpcDef{}, gachaRefusalNoObject, fmt.Sprintf("gid %d names no roster NPC", gid)
	}
	if !rt.npcVisibleTo(divisionID, character, npc) {
		return simulation.NpcDef{}, gachaRefusalOutOfScope, fmt.Sprintf("gid %d is outside the viewer's sector scope", gid)
	}
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return simulation.NpcDef{}, gachaRefusalTooFar, fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	return npc, 0, ""
}

// gachaOperatorCodename is the exchange operator Gori (RefObj 9252).
const gachaOperatorCodename = "NPC_CH_GACHA_OPERATOR"

/*
================
ConfigureGacha

ConfigureGacha loads the authoritative v1.150 Gacha tables and card refs.
Construction fails rather than registering a ticket-consuming opcode over
incomplete data.
================
*/
func (rt *Runtime) ConfigureGacha(textdataDir string) error {
	catalog, err := gacha.LoadCatalog(textdataDir, rt.deps.ItemReferences())
	if err != nil {
		return err
	}
	rt.GachaPoolV2 = GachaPoolV2FromEnv()
	if rt.GachaPoolV2 {
		if err := catalog.ApplyPoolV2(rt.deps.ItemReferences()); err != nil {
			return err
		}
		log.Infof("gacha: port-only prize pool v2 active (%s); the shipped gachaitemset.txt is untouched", EnvGachaPoolV2)
	}
	rt.GachaCatalog = catalog
	return nil
}

/*
================
secureGachaRoll
================
*/
func secureGachaRoll() (uint32, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1<<15))
	if err != nil {
		return 0, fmt.Errorf("gacha: random source: %w", err)
	}
	// Native rand() returns 15 bits; the reference server takes %10000.
	// Keep its distribution while retaining the server-owned secure source.
	return uint32(value.Uint64()) % gachaRollDenominator, nil
}

/*
================
registerGacha
================
*/
func (rt *Runtime) registerGacha(hub *transport.Hub) {
	hub.Handle(gacha.OpRoll, func(
		session *transport.Session,
		opcode uint16,
		payload []byte,
	) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("gacha: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		frames, refusal := rt.HandleGachaRoll(divisionID, character, payload)
		if refusal != "" {
			log.Debugf("gacha: 0x7053 refused for %s: %s", character.Name, refusal)
		}
		// A refusal carries its one [2][code] frame: dropping it would strand
		// the client in its waiting phase until relog.
		sendFrames(session, frames)
	})
	hub.Handle(gacha.OpExchange, func(
		session *transport.Session,
		opcode uint16,
		payload []byte,
	) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("gacha: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		frames, refusal := rt.HandleGachaExchange(divisionID, character, payload)
		if refusal != "" {
			log.Debugf("gacha: 0x71BE refused for %s: %s", character.Name, refusal)
		}
		sendFrames(session, frames)
	})
}

/*
================
HandleGachaNpcAction

HandleGachaNpcAction accepts only the exact action-0x27 native composition:
0x7338 [boundGid][0x10000]. It must name the actor's currently selected,
live roster NPC and that NPC must occur in gachanpcmap.txt.
================
*/
func (rt *Runtime) HandleGachaNpcAction(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil {
		return nil, "Gacha catalogue is unavailable"
	}
	gid, actionFlags, err := gacha.DecodeNpcAction(payload)
	if err != nil {
		return nil, err.Error()
	}
	if actionFlags != gacha.InteractionFlagGacha {
		return nil, fmt.Sprintf("unsupported action flags 0x%X", actionFlags)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		return nil, "bound NPC is not the current selected object"
	}
	npc, ok := rt.gachaNpcForGID(divisionID, character, gid)
	if !ok {
		return nil, fmt.Sprintf("gid %d is not a live Gacha roster NPC", gid)
	}
	if !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return nil, fmt.Sprintf("NPC RefObjID %d has no v1.150 Gacha set", npc.RefObjID)
	}
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	return []wire.Frame{{
		Opcode:  gacha.OpInteractionState,
		Payload: gacha.EncodeOpenInteraction(),
	}}, ""
}

/*
================
HandleGachaRoll

HandleGachaRoll applies one atomic Magic Pop transaction. The request
names the machine gid, the gachaitemset entry and the ticket slot.
Admission is the native one (gachaNpcAdmission, then gacha set, bag limit,
ticket row, entry lookup); every refusal answers one B053 [2][u8 code],
the native answer the client's waiting phase ends on. On commit the ticket
row becomes a native win/loss result card; 0x3645 reaches the client before
B053 so CIFGhaCha_ApplyResult observes the new ref and reward map.
================
*/
func (rt *Runtime) HandleGachaRoll(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil || rt.GachaRoll == nil {
		return gachaRefusal(gacha.OpResult, gachaRefusalFailed), "Gacha catalogue or random source is unavailable"
	}
	request, err := gacha.DecodeRollRequest(payload)
	if err != nil {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot), err.Error()
	}
	// The wire shape bound; the authority door checks the character's capacity.
	if request.InventorySlot < inventory.EquipmentSlotEnd ||
		request.InventorySlot >= inventory.MaxBagEnd {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d is outside the bag", request.InventorySlot)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	npc, code, reason := rt.gachaNpcAdmission(divisionID, character, request.BoundNpcGID)
	if reason != "" {
		return gachaRefusal(gacha.OpResult, code), reason
	}
	if !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return gachaRefusal(gacha.OpResult, gachaRefusalNoService),
			fmt.Sprintf("NPC RefObjID %d has no v1.150 Gacha set", npc.RefObjID)
	}
	if !inventory.IsBagSlot(request.InventorySlot, inventory.BagEnd(character)) {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d is beyond the bag limit", request.InventorySlot)
	}
	rowIndex := -1
	for index := range character.MissionInventory {
		if character.MissionInventory[index].Slot == int64(request.InventorySlot) {
			rowIndex = index
			break
		}
	}
	if rowIndex < 0 {
		return gachaRefusal(gacha.OpResult, gachaRefusalEmptySlot),
			fmt.Sprintf("inventory slot %d is empty", request.InventorySlot)
	}
	if row := &character.MissionInventory[rowIndex]; row.RefObjID != rt.GachaCatalog.Card.RefObjID ||
		row.TypeFlags != rt.GachaCatalog.Card.TypeFlags ||
		row.Codename != rt.GachaCatalog.Card.Codename {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d holds no Magic Pop ticket", request.InventorySlot)
	}
	prize, ok := rt.GachaCatalog.PrizeForNpc(npc.RefObjID, request.SelectedEntryID)
	if !ok {
		return gachaRefusal(gacha.OpResult, gachaRefusalNoEntry), fmt.Sprintf(
			"entry %d is not mapped to NPC RefObjID %d",
			request.SelectedEntryID,
			npc.RefObjID,
		)
	}

	failure := "ticket row changed before commit"
	var frames []wire.Frame
	rolledWin := false
	var rolledReward, rolledQuantity uint32
	committed := rt.deps.Update(character, "gacha-roll", func() bool {
		if character.DeletePending || !inventory.IsBagSlot(request.InventorySlot, inventory.BagEnd(character)) {
			return false
		}
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.InventorySlot) {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		row := &character.MissionInventory[rowIndex]
		if row.RefObjID != rt.GachaCatalog.Card.RefObjID ||
			row.TypeFlags != rt.GachaCatalog.Card.TypeFlags ||
			row.Codename != rt.GachaCatalog.Card.Codename {
			return false
		}

		roll, err := rt.GachaRoll()
		if err != nil {
			failure = err.Error()
			return false
		}
		if roll >= gachaRollDenominator {
			failure = "Gacha random source returned an out-of-range value"
			return false
		}
		win := roll < prize.ChancePer10000
		if !win {
			sample, err := rt.GachaRoll()
			if err != nil {
				failure = err.Error()
				return false
			}
			waste, ok := rt.GachaCatalog.WastePrizeForNpc(npc.RefObjID, sample)
			if !ok {
				failure = "Gacha waste draw is unavailable or out of range"
				return false
			}
			prize = waste
		}

		rolledWin = win
		rolledReward, rolledQuantity = prize.RewardRefObjID, prize.Quantity
		result := rt.GachaCatalog.LoseCard
		code := gacha.ResultLose
		if win {
			result = rt.GachaCatalog.WinCard
			code = gacha.ResultWin
		}
		// Natively both parameters are written for either result card.
		magicOptions := []uint64{uint64(prize.RewardRefObjID), uint64(prize.Quantity)}
		delta := gacha.EncodeResultItemDelta(request.InventorySlot, result.RefObjID, prize.RewardRefObjID, prize.Quantity)
		*row = enterworld.InventoryRow{
			Slot:         int64(request.InventorySlot),
			RefObjID:     result.RefObjID,
			Codename:     result.Codename,
			TypeFlags:    result.TypeFlags,
			VarianceBits: "0",
			StackCount:   1,
			MagicOptions: magicOptions,
		}
		frames = []wire.Frame{
			{Opcode: gacha.OpItemStateDelta, Payload: delta},
			{Opcode: gacha.OpResult, Payload: gacha.EncodeResult(code)},
		}
		return true
	})
	if !committed {
		return gachaRefusal(gacha.OpResult, gachaRefusalFailed), failure
	}
	if rt.GachaPoolV2 {
		outcome := "lose"
		if rolledWin {
			outcome = "win"
		}
		log.Infof("gacha pool v2: %s rolled entry %d at NPC %d: %s, reward %d x%d",
			character.Name, request.SelectedEntryID, npc.RefObjID, outcome, rolledReward, rolledQuantity)
	}
	return frames, ""
}

/*
================
gachaNpcForGID
================
*/
func (rt *Runtime) gachaNpcForGID(
	divisionID string,
	character *enterworld.Character,
	gid uint32,
) (simulation.NpcDef, bool) {
	// Use the same live world/region admission as the generic NPC owner.
	// A selected GID can survive travel; roster presence alone is insufficient.
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	if !ok || !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return simulation.NpcDef{}, false
	}
	return npc, true
}

/*
================
prepareGachaRewardItem

Natively the voucher mutates to the prize item and the special equipment
property roll runs with a zeroed override spec
(all 26 v1.150 gachaitemset.txt rows carry param1..4 == 0). Non-equipment
returns immediately without drawing from DropRoll; equipment (TID 3.1.*.*)
rolls a low-biased +1..+5 enhancement level (minimum of two 1..5 draws),
5-bit white-stat variance + tier-0 durability, and blue magic
options (100% entry for Rarity == 2 Seal of Star, 31/101 for
normal gear).
================
*/
func (rt *Runtime) prepareGachaRewardItem(reward *enterworld.ItemRef, quantity uint64) (inventory.Item, error) {
	if wire.IsGoldHeap(reward.TypeFlags()) {
		return inventory.Item{}, fmt.Errorf("ground-only item %s cannot enter an inventory", reward.Codename)
	}
	item := inventory.Item{
		RefObjID:  reward.RefObjID,
		Codename:  reward.Codename,
		TypeFlags: reward.TypeFlags(),
		Quantity:  uint16(quantity),
	}
	if reward.TypeIDs[0] == 3 && reward.TypeIDs[1] == 1 {
		plus := uint8(5)
		for i := 0; i < 2; i++ {
			value, ok := rt.rollUniqueCount(1, 5)
			if !ok {
				return inventory.Item{}, fmt.Errorf("gacha equipment plus roll failed for %s", reward.Codename)
			}
			plus = min(plus, uint8(value))
		}
		variance, durability, ok := rt.rollDroppedEquipmentVariance(reward)
		if !ok {
			return inventory.Item{}, fmt.Errorf("gacha equipment variance roll failed for %s", reward.Codename)
		}
		magic, _, err := loot.EquipmentMagic(reward.Codename, rt.DropRoll)
		if err != nil {
			return inventory.Item{}, fmt.Errorf("gacha equipment magic roll failed for %s: %w", reward.Codename, err)
		}
		item.Plus = plus
		item.VarianceBits = variance
		item.Durability = durability
		item.MagicOptions = magic
	}
	return item, nil
}

/*
================
HandleGachaExchange

HandleGachaExchange answers 0x71BE [operatorGid] from the client's talk row 5DB67B
sends once 642270 found a voucher in the bag. Per the reference server's exchange,
the lowest-slot win or loss voucher is consumed and its recorded pair,
reference id then quantity, becomes bag items with the native roll giving +1..+5
OptLevel, 5-bit white variance + durability, and blue magic options on
equipment rewards. The voucher leaves by a 0x3645 quantity of zero rather
than a row delete, because that is the update client 7654B0 turns into
UIIT_MSG_GACHA_EXCHANGE.

The removal is pinned to the slot whose pair was read, never to the codename:
a same-codename card without a pair in a lower slot would otherwise be eaten
while the paid voucher stayed to be redeemed again. A stacked row or a pair
that is not an enabled prize row is refused, so the exchange can only hand out
what a roll could have written.
================
*/
func (rt *Runtime) HandleGachaExchange(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalFailed), "Gacha catalogue is unavailable"
	}
	gid, err := gacha.DecodeExchangeRequest(payload)
	if err != nil {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalBadSlot), err.Error()
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	// The native admission (gachaNpcAdmission), then the operator service:
	// natively an NPC without it refuses 0x1C0F and one with no exchangeable
	// voucher refuses 5, each answered [2][u8 code] on 0xB1BE.
	npc, code, reason := rt.gachaNpcAdmission(divisionID, character, gid)
	if reason != "" {
		return gachaRefusal(gacha.OpExchangeResult, code), reason
	}
	if npc.Codename != gachaOperatorCodename {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalNoService),
			fmt.Sprintf("gid %d is not a live Gacha exchange operator", gid)
	}

	failure := "no exchangeable voucher"
	var frames []wire.Frame
	committed := rt.deps.Update(character, "gacha-exchange", func() bool {
		if character.DeletePending || rt.deps.ItemReferences() == nil {
			return false
		}
		voucher := -1
		for index, row := range character.MissionInventory {
			if row.Slot < int64(inventory.EquipmentSlotEnd) || !inventory.IsBagSlot(uint8(row.Slot), inventory.BagEnd(character)) ||
				!rt.GachaCatalog.IsResultCard(row.RefObjID) || len(row.MagicOptions) != 2 {
				continue
			}
			if voucher < 0 || row.Slot < character.MissionInventory[voucher].Slot {
				voucher = index
			}
		}
		if voucher < 0 {
			return false
		}
		row := character.MissionInventory[voucher]
		if row.StackCount != 1 {
			failure = fmt.Sprintf("voucher row at slot %d is stacked (%d)", row.Slot, row.StackCount)
			return false
		}
		rewardRef, quantity := uint32(row.MagicOptions[0]), row.MagicOptions[1]
		if quantity < 1 || quantity > 0xFFFF || uint64(rewardRef) != row.MagicOptions[0] ||
			!rt.GachaCatalog.IsPrizePair(rewardRef, quantity) {
			failure = "voucher carries an invalid reward pair"
			return false
		}
		reward, found := rt.deps.ItemReferences().ItemRefByID(rewardRef)
		if !found || reward == nil {
			failure = fmt.Sprintf("voucher reward %d is unresolved", rewardRef)
			return false
		}
		slot := uint8(row.Slot)
		before := invItemsFromBag(character)
		inv := inventory.New(before, inventory.BagEnd(character))
		if _, fault := inv.DropQuantity(slot, 1); fault != nil {
			failure = fmt.Sprintf("voucher at slot %d: %v", slot, fault)
			return false
		}
		rewardItem, err := rt.prepareGachaRewardItem(reward, quantity)
		if err != nil {
			failure = err.Error()
			return false
		}
		rows, planned, err := rt.planInventoryItemsGrant(
			character,
			before,
			inv,
			[]inventory.Item{rewardItem},
			map[uint8]bool{slot: true},
		)
		if err != nil {
			failure = err.Error()
			return false
		}
		character.MissionInventory = rows
		// The native client wants the voucher's quantity update, not a row delete,
		// and first: the freed slot is the first free one, so the reward usually
		// lands in it, and client 7654B0 must still find the voucher there.
		quantityFrame := wire.Frame{
			Opcode:  gacha.OpItemStateDelta,
			Payload: wire.NewWriter(4).U8(slot).U8(8).U16(0).Payload(),
		}
		frames = append([]wire.Frame{quantityFrame}, planned...)
		return true
	})
	if !committed {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalFailed), failure
	}
	return frames, ""
}
