/*
===========================================================================

exchange.go - player-to-player exchange: request, offers and the swap

CGObjPC_HandleExchangeRequest7081 (5154E0) admits a request to a PC within
320 units on an adjacent sector (3: no such PC or oneself, 4: too far);
the target answers the 0x3393 kind-1 prompt. The session (package
item/exchange) then takes bag slots and gold from each side through the
item moves 4, 5 and 0x0D, confirm (7082), approve (7083) and cancel
(7084). The second approval swaps both offers in one authority
transaction: each player's offered items leave its bag and the partner's
fill its first empty bag slots in exchange-slot order, as the client's
0x3272 handler (765260) places them. Bags that cannot take the incoming
items fail the exchange (0x22 for the side that is full, 0x36 for its
partner).

INFERENCE: the GameServer's ExchangeMgr is not decompiled past the
handlers, so three rules follow the client and the v1.188 texts:

  - an item whose RefObjCommon CanTrade (token 16) is 0, a summoned
    COS's item or a worn item cannot be offered (0x3A);
  - an offer cannot change after its side confirmed, and approval waits
    for both confirmations;
  - leaving, disconnecting or teleporting ends the session (0x2B to the
    partner), and other item moves are refused while it is open.

===========================================================================
*/
package action

import (
	"errors"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/exchange"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

const (
	// exchangeReach is 5154E0's `Vec3_Length < 320`.
	exchangeReach = 320

	// exchangePromptKind is the 0x3393 kind of an exchange request
	// (7644E0 case 0 opens confirm box 7).
	exchangePromptKind uint8 = 1
)

/*
================
HandleExchangeRequest

0x7237 [u32 target gid].
================
*/
func (rt *Runtime) HandleExchangeRequest(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if err != nil || r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refuse := func(code uint8) OpResult {
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpExchangeRequestResult, Payload: wire.EncodeExchangeResult(code)}}}
	}
	target := rt.findCharacterByGid(division, gid)
	if target == nil || target.ID == c.ID || target.DeletePending {
		return refuse(wire.ExchangeErrInvalidTarget)
	}
	nowMs := rt.Now().UnixMilli()
	if rt.Exchanges.Trading(division, c.Name) || rt.Exchanges.Trading(division, target.Name) ||
		rt.Exchanges.Pending(division, target.Name, nowMs) ||
		rt.ProposalPending != nil && rt.ProposalPending(division, target.Name) {
		return refuse(wire.ExchangeErrBusy)
	}
	if !rt.exchangeInReach(division, c, target) {
		return refuse(wire.ExchangeErrTooFar)
	}
	rt.Exchanges.Propose(division, c.Name, target.Name, nowMs)
	prompt := wire.NewWriter(5).U8(exchangePromptKind).U32(enterworld.ObjectIDForCharacter(c)).Payload()
	rt.PushCharacterFrames(division, target.Name, []wire.Frame{{Opcode: opInvitationPrompt, Payload: prompt}})
	return OpResult{}
}

// opInvitationPrompt is the shared 0x3393 proposal opcode.
const opInvitationPrompt uint16 = 0x3393

/*
================
exchangeInReach
================
*/
func (rt *Runtime) exchangeInReach(division string, a, b *enterworld.Character) bool {
	from, to := rt.LiveSpawnFor(division, a), rt.LiveSpawnFor(division, b)
	return samePlaneAdjacent(from, to) && distance3D(from, to) < exchangeReach
}

//============================================================================

/*
================
ExchangeConsent

The exchange lane on the shared 0x3393 answer (party.ConsentArm,
implemented structurally).
================
*/
type ExchangeConsent struct {
	rt *Runtime
}

/*
================
ExchangeConsent
================
*/
func (rt *Runtime) ExchangeConsent() *ExchangeConsent {
	return &ExchangeConsent{rt: rt}
}

/*
================
HasPendingInvite
================
*/
func (c *ExchangeConsent) HasPendingInvite(divisionID, name string) bool {
	return c.rt.Exchanges.Pending(divisionID, name, c.rt.Now().UnixMilli())
}

/*
================
DropPendingInvite
================
*/
func (c *ExchangeConsent) DropPendingInvite(divisionID, name string) bool {
	return c.rt.Exchanges.DropRequest(divisionID, name)
}

/*
================
ApplyConsent

Box 7 answers {1, 1} to accept; anything else refuses (0x28 to the
requester).
================
*/
func (c *ExchangeConsent) ApplyConsent(_ *transport.Session, division string, actor *enterworld.Character, first, second uint8) {
	rt := c.rt
	if actor == nil {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	request, ok := rt.Exchanges.TakeRequest(division, actor.Name, rt.Now().UnixMilli())
	if !ok {
		return
	}
	requester := rt.findCharacter(division, request.From)
	if requester == nil {
		return
	}
	answer := func(code uint8, body ...byte) {
		rt.PushCharacterFrames(division, requester.Name, []wire.Frame{{
			Opcode:  wire.OpExchangeRequestResult,
			Payload: wire.EncodeExchangeResult(code, body...),
		}})
	}
	if first != 1 || second != 1 {
		answer(wire.ExchangeErrDenied)
		return
	}
	if rt.Exchanges.Trading(division, requester.Name) || rt.Exchanges.Trading(division, actor.Name) {
		answer(wire.ExchangeErrBusy)
		return
	}
	if !rt.exchangeInReach(division, requester, actor) {
		answer(wire.ExchangeErrTooFar)
		return
	}
	rt.Exchanges.Open(division, requester.Name, actor.Name)
	answer(0, wire.EncodeExchangeGid(enterworld.ObjectIDForCharacter(actor))...)
	rt.PushCharacterFrames(division, actor.Name, []wire.Frame{{
		Opcode:  wire.OpExchangeOpened,
		Payload: wire.EncodeExchangeGid(enterworld.ObjectIDForCharacter(requester)),
	}})
}

//============================================================================

/*
================
applyExchangeMove

The 0x706D moves 4 (bag -> table), 5 (table -> bag) and 0x0D (gold).
Called under the division lock from HandleItemMove.
================
*/
func (rt *Runtime) applyExchangeMove(division string, c *enterworld.Character, request wire.ItemMoveRequest) OpResult {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || !rt.Exchanges.Trading(division, c.Name) {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	var frame []byte
	switch request.MovementType {
	case wire.MoveTypeExchangePut:
		item, code := rt.exchangeOfferable(snapshot, request.SourceSlot)
		if code != 0 {
			return failureResult(code)
		}
		slot, err := rt.Exchanges.AddItem(division, c.Name, exchange.Offer{BagSlot: request.SourceSlot, RefObjID: item.RefObjID, Quantity: item.Quantity})
		if err != nil {
			return failureResult(exchangeMoveCode(err))
		}
		frame = wire.EncodeExchangePutResult(request.SourceSlot, slot)
	case wire.MoveTypeExchangeTake:
		if err := rt.Exchanges.RemoveItem(division, c.Name, request.SourceSlot); err != nil {
			return failureResult(exchangeMoveCode(err))
		}
		frame = wire.EncodeExchangeTakeResult(request.SourceSlot)
	case wire.MoveTypeExchangeGold:
		if uint64(request.GoldAmount) > goldOf(snapshot) {
			return failureResult(wire.ErrCodeNotEnoughGold)
		}
		if err := rt.Exchanges.SetGold(division, c.Name, uint64(request.GoldAmount)); err != nil {
			return failureResult(exchangeMoveCode(err))
		}
		_, partner, _ := rt.Exchanges.Snapshot(division, c.Name)
		rt.PushCharacterFrames(division, partner.Name, []wire.Frame{{
			Opcode:  wire.OpExchangePartnerGold,
			Payload: wire.EncodeExchangePartnerGold30BB(request.GoldAmount),
		}})
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodeExchangeGoldResult(request.GoldAmount)}}}
	default:
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	rt.publishExchangeOffer(division, snapshot)
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: frame}}}
}

/*
================
exchangeOfferable

A bag item that may go on the table.
================
*/
func (rt *Runtime) exchangeOfferable(c *enterworld.Character, slot uint8) (inventory.Item, uint8) {
	if slot < inventory.EquipmentSlotEnd || slot >= inventory.BagEnd(c) {
		return inventory.Item{}, wire.ExchangeErrCannotTrade
	}
	item, ok := bagOf(c).At(slot)
	if !ok {
		return inventory.Item{}, wire.ErrCodeInvalidRequest
	}
	if item.Summon != nil && item.Summon.Summoned {
		return inventory.Item{}, wire.ExchangeErrCannotTrade
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return inventory.Item{}, wire.ExchangeErrCannotTrade
	}
	if value, present := ref.NativeFields.Lookup("canTrade"); !present || value < 1 {
		return inventory.Item{}, wire.ExchangeErrCannotTrade
	}
	return item, 0
}

/*
================
exchangeMoveCode
================
*/
func exchangeMoveCode(err error) uint8 {
	switch {
	case errors.Is(err, exchange.ErrFull):
		return wire.ExchangeErrBayFull
	case errors.Is(err, exchange.ErrNoSession):
		return wire.ErrCodeInvalidRequest
	}
	return wire.ErrCodeInvalidRequest
}

/*
================
publishExchangeOffer

The owner's side to both players: the owner's list carries bag slots.
================
*/
func (rt *Runtime) publishExchangeOffer(division string, owner *enterworld.Character) {
	own, partner, ok := rt.Exchanges.Snapshot(division, owner.Name)
	if !ok {
		return
	}
	bag := bagOf(owner)
	var rows []wire.ExchangeOfferRow
	for slot, offer := range own.Offers {
		if offer == nil {
			continue
		}
		if item, ok := bag.At(offer.BagSlot); ok {
			rows = append(rows, wire.ExchangeOfferRow{BagSlot: offer.BagSlot, ExchangeSlot: uint8(slot), Item: item.Body()})
		}
	}
	gid := enterworld.ObjectIDForCharacter(owner)
	rt.PushCharacterFrames(division, owner.Name, []wire.Frame{{Opcode: wire.OpExchangeOffer, Payload: wire.EncodeExchangeOffer3569(gid, rows, true)}})
	rt.PushCharacterFrames(division, partner.Name, []wire.Frame{{Opcode: wire.OpExchangeOffer, Payload: wire.EncodeExchangeOffer3569(gid, rows, false)}})
}

//============================================================================

/*
================
HandleExchangeConfirm

0x7095: locks the offer; the partner hears 0x37CF.
================
*/
func (rt *Runtime) HandleExchangeConfirm(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if err := rt.Exchanges.Confirm(division, c.Name); err != nil {
		return exchangeAnswer(wire.OpExchangeConfirmResult, wire.ExchangeErrCancelled)
	}
	_, partner, _ := rt.Exchanges.Snapshot(division, c.Name)
	rt.PushCharacterFrames(division, partner.Name, []wire.Frame{{Opcode: wire.OpExchangePartnerLocked}})
	return exchangeAnswer(wire.OpExchangeConfirmResult, 0)
}

/*
================
HandleExchangeApprove

0x734A: the second approval swaps the offers.
================
*/
func (rt *Runtime) HandleExchangeApprove(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	both, err := rt.Exchanges.Approve(division, c.Name)
	if err != nil {
		return exchangeAnswer(wire.OpExchangeApproveResult, wire.ExchangeErrCancelled)
	}
	if !both {
		return exchangeAnswer(wire.OpExchangeApproveResult, 0)
	}
	rt.commitExchange(division, c)
	return OpResult{}
}

/*
================
HandleExchangeCancel

0x72DB: ends the session; the partner hears it cancelled (0x2C).
================
*/
func (rt *Runtime) HandleExchangeCancel(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	partner, ok := rt.Exchanges.Close(division, c.Name)
	if !ok {
		return OpResult{}
	}
	rt.PushCharacterFrames(division, partner, []wire.Frame{{Opcode: wire.OpExchangeFailed, Payload: []byte{wire.ExchangeErrCancelledByPeer}}})
	return exchangeAnswer(wire.OpExchangeCancelResult, 0)
}

/*
================
AbandonExchange

A player who leaves, disconnects or teleports ends the session; the
partner hears it fail (0x2B). The caller holds no division lock.
================
*/
func (rt *Runtime) AbandonExchange(division, name string) {
	if rt == nil || rt.Exchanges == nil {
		return
	}
	rt.Exchanges.DropRequest(division, name)
	partner, ok := rt.Exchanges.Close(division, name)
	if ok && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, partner, []wire.Frame{{Opcode: wire.OpExchangeFailed, Payload: []byte{wire.ExchangeErrCancelled}}})
	}
}

/*
================
exchangeAnswer
================
*/
func exchangeAnswer(opcode uint16, code uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: wire.EncodeExchangeResult(code)}}}
}

//============================================================================

/*
================
commitExchange

Swaps both offers in one transaction and ends the session. Each bag
first takes the partner's items in its first empty slots, then gives up
its own (765260's order), so incoming items need that many free slots.
================
*/
func (rt *Runtime) commitExchange(division string, c *enterworld.Character) {
	own, partnerSide, ok := rt.Exchanges.Snapshot(division, c.Name)
	if !ok {
		return
	}
	partner := rt.findCharacter(division, partnerSide.Name)
	rt.Exchanges.Close(division, c.Name)
	fail := func(codeOwn, codePartner uint8) {
		rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: wire.OpExchangeFailed, Payload: []byte{codeOwn}}})
		if partner != nil {
			rt.PushCharacterFrames(division, partner.Name, []wire.Frame{{Opcode: wire.OpExchangeFailed, Payload: []byte{codePartner}}})
		}
	}
	if partner == nil {
		fail(wire.ExchangeErrCancelled, wire.ExchangeErrCancelled)
		return
	}
	codeOwn, codePartner := uint8(0), uint8(0)
	committed := rt.deps.UpdateMany([]*enterworld.Character{c, partner}, "exchange", func() bool {
		if c.DeletePending || partner.DeletePending {
			codeOwn, codePartner = wire.ExchangeErrCancelled, wire.ExchangeErrCancelled
			return false
		}
		mine, theirs := bagOf(c), bagOf(partner)
		outMine, okMine := takeOffered(mine, own)
		outTheirs, okTheirs := takeOffered(theirs, partnerSide)
		if !okMine || !okTheirs || own.Gold > goldOf(c) || partnerSide.Gold > goldOf(partner) {
			codeOwn, codePartner = wire.ExchangeErrCancelled, wire.ExchangeErrCancelled
			return false
		}
		if !grantAll(mine, outTheirs) {
			codeOwn, codePartner = wire.ExchangeErrInventoryFull, wire.ExchangeErrPartnerSpace
			return false
		}
		if !grantAll(theirs, outMine) {
			codeOwn, codePartner = wire.ExchangeErrPartnerSpace, wire.ExchangeErrInventoryFull
			return false
		}
		if !removeOffered(mine, own) || !removeOffered(theirs, partnerSide) {
			codeOwn, codePartner = wire.ExchangeErrCancelled, wire.ExchangeErrCancelled
			return false
		}
		c.MissionInventory = rowsFromInvItems(mine.Items())
		partner.MissionInventory = rowsFromInvItems(theirs.Items())
		setGold(c, goldOf(c)-own.Gold+partnerSide.Gold)
		setGold(partner, goldOf(partner)-partnerSide.Gold+own.Gold)
		return true
	})
	if !committed {
		if codeOwn == 0 {
			codeOwn, codePartner = wire.ExchangeErrCancelled, wire.ExchangeErrCancelled
		}
		fail(codeOwn, codePartner)
		return
	}
	for _, who := range []*enterworld.Character{c, partner} {
		frames := []wire.Frame{
			{Opcode: wire.OpExchangeApproveResult, Payload: wire.EncodeExchangeResult(0)},
			{Opcode: wire.OpExchangeSucceeded},
		}
		if own.Gold != 0 || partnerSide.Gold != 0 {
			frames = append(frames, goldFrame(rt.characterSnapshot(division, who)))
		}
		rt.PushCharacterFrames(division, who.Name, frames)
	}
	log.Debugf("exchange: %s and %s swapped their offers", c.Name, partner.Name)
}

/*
================
takeOffered

Copies of the offered rows in exchange-slot order; false when an offered
slot no longer holds what was offered.
================
*/
func takeOffered(bag *inventory.Inventory, side exchange.Side) ([]inventory.Item, bool) {
	var out []inventory.Item
	for _, offer := range side.Offers {
		if offer == nil {
			continue
		}
		item, ok := bag.At(offer.BagSlot)
		if !ok || item.RefObjID != offer.RefObjID || item.Quantity != offer.Quantity {
			return nil, false
		}
		out = append(out, item)
	}
	return out, true
}

/*
================
grantAll
================
*/
func grantAll(bag *inventory.Inventory, items []inventory.Item) bool {
	for _, item := range items {
		if _, fault := bag.Grant(item); fault != nil {
			return false
		}
	}
	return true
}

/*
================
removeOffered
================
*/
func removeOffered(bag *inventory.Inventory, side exchange.Side) bool {
	for _, offer := range side.Offers {
		if offer == nil {
			continue
		}
		if _, fault := bag.Drop(offer.BagSlot); fault != nil {
			return false
		}
	}
	return true
}
