/*
===========================================================================

stall.go - street stalls: opening, visiting, editing and selling

CGObjPC_HandleStallCreate70B1 (515F10) opens a stall for a standing PC
outside a job suit; the stall shows nearby (0x30DF) and on the spawn row of
everyone who comes into view (the title block 86A26D reads). A visitor
within 100 units (516270: Vec3_Length <= 100) sees its greeting, offers and
visitors; while the stall is open for business (the owner's 0x71A8 kind 5)
a visitor buys a slot (CFleaMarket 472700: 0x3C2E closed, 0x3C10 empty
slot, 0x3C11 short of gold, 0x3C12 full bag). The owner edits only while it
is closed, apart from the mode, the open flag and the greeting (472E20).
The sale moves the slot's count out of the owner's bag into the buyer's
first empty bag slot and the price the other way, in one transaction, as
the buyer's client places it (5A3870).

INFERENCE: four rules follow the v1.150 texts where the GameServer's codes
have none:

  - a closed stall answers UIIT_MSG_FLEAMARKET_ERR_MARKET_CLOSED (0x0E)
    and an empty one cannot open (NOTHING_TO_SELL, 0x0C);
  - an item whose RefObjCommon CanTrade is 0 or a summoned COS's item
    cannot go on the table (INVALID_OPERATION, 0x05);
  - stalls open only in a world that lives for good (an instance answers
    IM_BUSY, 0x16), not while riding (0x34) or in a job suit (0x3B);
  - the owner's bag is locked to the stall while it stands, and leaving,
    disconnecting or teleporting ends a stall or a visit.

===========================================================================
*/
package action

import (
	"errors"
	"unicode/utf8"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/stall"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

const (
	// stallVisitReach is 516270's Vec3_Length <= 100.
	stallVisitReach = 100
	// stallErrNotHost is 0x3C2A: the target keeps no stall.
	stallErrNotHost uint8 = 0x2A
)

/*
================
stallAnswer
================
*/
func stallAnswer(opcode uint16, code uint8, body ...byte) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: wire.EncodeStallResult(code, body...)}}}
}

/*
================
stallBusy

A PC in an exchange, a stall or a visit cannot start another.
================
*/
func (rt *Runtime) stallBusy(division, name string) bool {
	if rt.Exchanges.Trading(division, name) || rt.Stalls.Keeping(division, name) {
		return true
	}
	_, visiting := rt.Stalls.Visiting(division, name)
	return visiting
}

/*
================
HandleStallCreate

0x7049 [wstr title].
================
*/
func (rt *Runtime) HandleStallCreate(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	title, err := r.WStr()
	if err != nil || r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refuse := func(code uint8) OpResult { return stallAnswer(wire.OpStallCreateResult, code) }
	if n := utf8.RuneCountInString(title); n == 0 || n > stall.TextLimit {
		return refuse(wire.StallErrNameLength)
	}
	if rt.Stalls.Keeping(division, c.Name) {
		return refuse(wire.StallErrHasStall)
	}
	if rt.stallBusy(division, c.Name) {
		return refuse(wire.StallErrBusy)
	}
	if code := rt.stallPlaceRefusal(division, c); code != 0 {
		return refuse(code)
	}
	if err := rt.Stalls.Open(division, c.Name, title); err != nil {
		return refuse(wire.StallErrHasStall)
	}
	opened := wire.Frame{Opcode: wire.OpStallOpened, Payload: wire.EncodeStallOpened(enterworld.ObjectIDForCharacter(c), title, 0)}
	rt.PushDivisionPeerFrames(division, c.Name, []wire.Frame{opened})
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpStallCreateResult, Payload: wire.EncodeStallResult(0)}, opened}}
}

/*
================
stallPlaceRefusal

515F10's state tests: a living world, not riding, no job suit.
================
*/
func (rt *Runtime) stallPlaceRefusal(division string, c *enterworld.Character) uint8 {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return wire.StallErrBusy
	}
	if here, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(snapshot)).Definition()); !ok || here.NativeType != 0 {
		return wire.StallErrBusy
	}
	if rt.jobDressed(snapshot) {
		return wire.StallErrJob
	}
	if rt.characterRiding(division, snapshot) {
		return wire.StallErrRideState
	}
	return 0
}

/*
================
HandleStallClose

0x742C: the stall comes down; its visitors' windows close with it
(0x33D1 reaches everyone around).
================
*/
func (rt *Runtime) HandleStallClose(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if _, ok := rt.Stalls.Close(division, c.Name); !ok {
		return stallAnswer(wire.OpStallCloseResult, stallErrNotHost)
	}
	closed := wire.Frame{Opcode: wire.OpStallClosed, Payload: wire.EncodeStallClosed(enterworld.ObjectIDForCharacter(c))}
	rt.PushDivisionPeerFrames(division, c.Name, []wire.Frame{closed})
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpStallCloseResult, Payload: wire.EncodeStallResult(0)}, closed}}
}

//============================================================================

/*
================
HandleStallVisit

0x761F [u32 owner gid].
================
*/
func (rt *Runtime) HandleStallVisit(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if err != nil || r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refuse := func(code uint8) OpResult { return stallAnswer(wire.OpStallVisitResult, code) }
	owner := rt.findCharacterByGid(division, gid)
	if owner == nil || owner.ID == c.ID {
		return refuse(wire.StallErrInvalidTarget)
	}
	from, to := rt.LiveSpawnFor(division, c), rt.LiveSpawnFor(division, owner)
	if !samePlaneAdjacent(from, to) || distance3D(from, to) > stallVisitReach {
		return refuse(wire.StallErrTooFar)
	}
	if !rt.Stalls.Keeping(division, owner.Name) {
		return refuse(stallErrNotHost)
	}
	if rt.stallBusy(division, c.Name) {
		return refuse(wire.StallErrBusy)
	}
	s, err := rt.Stalls.Enter(division, c.Name, owner.Name)
	if err != nil {
		return refuse(stallErrNotHost)
	}
	var visitors []uint32
	for _, name := range s.Visitors {
		if visitor := rt.findCharacter(division, name); visitor != nil && visitor.ID != c.ID {
			visitors = append(visitors, enterworld.ObjectIDForCharacter(visitor))
		}
	}
	came := wire.Frame{Opcode: wire.OpStallEvent, Payload: wire.EncodeStallVisitor(wire.StallEventCame, enterworld.ObjectIDForCharacter(c))}
	rt.pushStall(division, s, c.Name, came)
	body := wire.EncodeStallVisit(gid, s.Greeting, s.Open, s.Mode, rt.stallOffers(division, owner, s), visitors)
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpStallVisitResult, Payload: body}}}
}

/*
================
HandleStallLeave

0x76E7.
================
*/
func (rt *Runtime) HandleStallLeave(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 0 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	s, err := rt.Stalls.Leave(division, c.Name)
	if err != nil {
		return stallAnswer(wire.OpStallLeaveResult, wire.StallErrNotVisiting)
	}
	rt.pushStall(division, s, "", wire.Frame{Opcode: wire.OpStallEvent, Payload: wire.EncodeStallVisitor(wire.StallEventLeft, enterworld.ObjectIDForCharacter(c))})
	return stallAnswer(wire.OpStallLeaveResult, 0)
}

/*
================
AbandonStall

A player who leaves, disconnects or teleports ends the stall it keeps or
the visit it pays. The caller holds no division lock.
================
*/
func (rt *Runtime) AbandonStall(division, name string) {
	if rt == nil || rt.Stalls == nil || rt.PushCharacterFrames == nil {
		return
	}
	if rt.Stalls.Keeping(division, name) {
		rt.Stalls.Close(division, name)
		if c := rt.findCharacter(division, name); c != nil && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(division, name, []wire.Frame{{Opcode: wire.OpStallClosed, Payload: wire.EncodeStallClosed(enterworld.ObjectIDForCharacter(c))}})
		}
		return
	}
	if s, err := rt.Stalls.Leave(division, name); err == nil {
		if c := rt.findCharacter(division, name); c != nil {
			rt.pushStall(division, s, "", wire.Frame{Opcode: wire.OpStallEvent, Payload: wire.EncodeStallVisitor(wire.StallEventLeft, enterworld.ObjectIDForCharacter(c))})
		}
	}
}

/*
================
pushStall

One frame to the owner and every visitor but except.
================
*/
func (rt *Runtime) pushStall(division string, s stall.Stall, except string, frame wire.Frame) {
	for _, name := range append([]string{s.Owner}, s.Visitors...) {
		if name != except {
			rt.PushCharacterFrames(division, name, []wire.Frame{frame})
		}
	}
}

/*
================
stallOffers

The stall's slots as the wire lists them, read from the owner's bag.
================
*/
func (rt *Runtime) stallOffers(division string, owner *enterworld.Character, s stall.Stall) []wire.StallOffer {
	snapshot := rt.characterSnapshot(division, owner)
	if snapshot == nil {
		return nil
	}
	bag := bagOf(snapshot)
	var out []wire.StallOffer
	for i, slot := range s.Slots {
		if slot == nil {
			continue
		}
		item, ok := bag.At(slot.BagSlot)
		if !ok {
			continue
		}
		body := item.Body()
		body.Quantity = slot.Quantity
		out = append(out, wire.StallOffer{Slot: uint8(i), Item: body, BagSlot: slot.BagSlot, Quantity: slot.Quantity, Price: slot.Price})
	}
	return out
}

//============================================================================

/*
================
HandleStallEdit

0x71A8 [u8 kind] and its body; the answer goes to everyone at the stall.
================
*/
func (rt *Runtime) HandleStallEdit(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	kind, err := r.U8()
	if err != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refuse := func(code uint8) OpResult { return stallAnswer(wire.OpStallEditResult, code) }
	current, ok := rt.Stalls.Get(division, c.Name)
	if !ok {
		return refuse(stallErrNotHost)
	}
	// 472E20: an open stall takes only the mode, the open flag and the
	// greeting.
	if current.Open && kind != wire.StallEditMode && kind != wire.StallEditOpen && kind != wire.StallEditGreeting {
		return refuse(wire.StallErrHostState)
	}
	body, code := rt.applyStallEdit(division, c, kind, r)
	if code != 0 {
		return refuse(code)
	}
	s, _ := rt.Stalls.Get(division, c.Name)
	answer := wire.Frame{Opcode: wire.OpStallEditResult, Payload: wire.EncodeStallEdit(kind, body)}
	rt.pushStall(division, s, c.Name, answer)
	if kind == wire.StallEditTitle {
		rt.PushDivisionPeerFrames(division, c.Name, []wire.Frame{{Opcode: wire.OpStallRenamed, Payload: wire.EncodeStallRenamed(enterworld.ObjectIDForCharacter(c), s.Title)}})
	}
	return OpResult{Frames: []wire.Frame{answer}}
}

/*
================
applyStallEdit

One edit kind; returns the 0xB1A8 body after the kind byte.
================
*/
func (rt *Runtime) applyStallEdit(division string, c *enterworld.Character, kind uint8, r *wire.Reader) ([]byte, uint8) {
	switch kind {
	case wire.StallEditModify:
		slot, _ := r.U8()
		count, _ := r.U16()
		price, _ := r.U32()
		_, _ = r.U8()
		if r.Done() != nil {
			return nil, wire.StallErrInvalidOp
		}
		if int(slot) >= stall.Slots {
			return nil, wire.StallErrBadSlot
		}
		code := uint8(0)
		serial := rt.Stalls.NextSerial()
		_, _ = rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			offer := s.Slots[slot]
			if offer == nil {
				code = wire.StallErrBadSlot
				return nil
			}
			if code = rt.stallCountRefusal(division, c, offer.BagSlot, count); code != 0 {
				return nil
			}
			offer.Quantity, offer.Price, offer.Serial = count, price, serial
			return nil
		})
		if code != 0 {
			return nil, code
		}
		return wire.NewWriter(8).U8(slot).U16(count).U32(price).U8(0).Payload(), 0
	case wire.StallEditAdd:
		slot, _ := r.U8()
		bagSlot, _ := r.U8()
		count, _ := r.U16()
		price, _ := r.U32()
		_, _ = r.U32()
		_, _ = r.U8()
		if r.Done() != nil {
			return nil, wire.StallErrInvalidOp
		}
		item, code := rt.stallOfferable(division, c, bagSlot)
		if code == 0 {
			code = rt.stallCountRefusal(division, c, bagSlot, count)
		}
		if code != 0 {
			return nil, code
		}
		offer := stall.Slot{BagSlot: bagSlot, RefObjID: item.RefObjID, Quantity: count, Price: price, Category: rt.stallCategory(item), Serial: rt.Stalls.NextSerial()}
		_, err := rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			for _, other := range s.Slots {
				if other != nil && other.BagSlot == bagSlot {
					return stall.ErrOffered
				}
			}
			if int(slot) >= stall.Slots || s.Slots[slot] != nil {
				free := -1
				for i, other := range s.Slots {
					if other == nil {
						free = i
						break
					}
				}
				if free < 0 {
					return stall.ErrFull
				}
				slot = uint8(free)
			}
			s.Slots[slot] = &offer
			return nil
		})
		switch {
		case errors.Is(err, stall.ErrFull):
			return nil, wire.StallErrStallFull
		case err != nil:
			return nil, wire.StallErrBadBagSlot
		}
		return rt.stallOfferBody(division, c), 0
	case wire.StallEditRemove:
		slot, _ := r.U8()
		_, _ = r.U8()
		if r.Done() != nil {
			return nil, wire.StallErrInvalidOp
		}
		_, err := rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			if int(slot) >= stall.Slots || s.Slots[slot] == nil {
				return stall.ErrBadSlot
			}
			s.Slots[slot] = nil
			return nil
		})
		if err != nil {
			return nil, wire.StallErrBadSlot
		}
		return rt.stallOfferBody(division, c), 0
	case wire.StallEditMode:
		mode, _ := r.U8()
		if r.Done() != nil || mode >= stall.ModeLimit {
			return nil, wire.StallErrHostState
		}
		_, _ = rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			s.Mode = mode
			return nil
		})
		return []byte{mode}, 0
	case wire.StallEditOpen:
		open, _ := r.U8()
		network, _ := r.U8()
		if r.Done() != nil || open > 1 {
			return nil, wire.StallErrInvalidOp
		}
		return rt.openStall(division, c, open == 1, network == 1)
	case wire.StallEditGreeting:
		greeting, err := r.WStr()
		if err != nil || r.Done() != nil || utf8.RuneCountInString(greeting) > stall.TextLimit {
			return nil, wire.StallErrGreeting
		}
		_, _ = rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			s.Greeting = greeting
			return nil
		})
		return wire.NewWriter(4 + 2*len(greeting)).WStr(greeting).Payload(), 0
	case wire.StallEditTitle:
		title, err := r.WStr()
		if n := utf8.RuneCountInString(title); err != nil || r.Done() != nil || n == 0 || n > stall.TextLimit {
			return nil, wire.StallErrNameLength
		}
		_, _ = rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
			s.Title = title
			return nil
		})
		return nil, 0
	}
	return nil, wire.StallErrInvalidOp
}

/*
================
openStall

Kind 5: opens or closes the stall for business; an opening that asks for
the stall network registers it there when the owner stands in a town
(code 1), and answers 0x4A outside one, leaving it unlisted.
================
*/
func (rt *Runtime) openStall(division string, c *enterworld.Character, open, network bool) ([]byte, uint8) {
	networkCode := uint8(0)
	_, err := rt.Stalls.Edit(division, c.Name, func(s *stall.Stall) error {
		if open {
			empty := true
			for _, slot := range s.Slots {
				if slot != nil {
					empty = false
				}
			}
			if empty {
				return stall.ErrClosed
			}
		}
		s.Open = open
		s.Network = false
		if open && network {
			if rt.inTown(division, c) {
				s.Network = true
				networkCode = wire.StallNetworkRegistered
			} else {
				networkCode = wire.StallErrRegisterTown
			}
		}
		return nil
	})
	if err != nil {
		return nil, wire.StallErrNothingToSell
	}
	return []byte{boolU8(open), networkCode}, 0
}

/*
================
boolU8
================
*/
func boolU8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

/*
================
stallOfferBody

The 0xB1A8 kind 2 and 3 body: a zero code and the offers.
================
*/
func (rt *Runtime) stallOfferBody(division string, c *enterworld.Character) []byte {
	s, _ := rt.Stalls.Get(division, c.Name)
	w := wire.NewWriter(64).U8(0)
	return wire.WriteStallOffers(w, rt.stallOffers(division, c, s)).Payload()
}

/*
================
stallOfferable

A bag item that may go on the table (the exchange's rule).
================
*/
func (rt *Runtime) stallOfferable(division string, c *enterworld.Character, bagSlot uint8) (inventory.Item, uint8) {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return inventory.Item{}, wire.StallErrBadBagSlot
	}
	item, code := rt.exchangeOfferable(snapshot, bagSlot)
	switch code {
	case 0:
		return item, 0
	case wire.ErrCodeInvalidRequest:
		return inventory.Item{}, wire.StallErrBadBagSlot
	}
	return inventory.Item{}, wire.StallErrInvalidOp
}

/*
================
stallCountRefusal

472E20: a count of at least one, no more than the bag stack holds.
================
*/
func (rt *Runtime) stallCountRefusal(division string, c *enterworld.Character, bagSlot uint8, count uint16) uint8 {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return wire.StallErrBadBagSlot
	}
	item, ok := bagOf(snapshot).At(bagSlot)
	if !ok {
		return wire.StallErrBadBagSlot
	}
	stack := item.Quantity
	if stack == 0 {
		stack = 1
	}
	if count == 0 || count > stack {
		return wire.StallErrBadCount
	}
	return 0
}

//============================================================================

/*
================
HandleStallBuy

0x73F9 [u8 slot] from a visitor.
================
*/
func (rt *Runtime) HandleStallBuy(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	slot, err := r.U8()
	if err != nil || r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	ownerName, visiting := rt.Stalls.Visiting(division, c.Name)
	if !visiting {
		return stallAnswer(wire.OpStallBuyResult, wire.StallErrNotVisiting)
	}
	code := rt.sellStallSlot(division, ownerName, slot, c, wire.StallEventSold, 0)
	if code != 0 {
		return stallAnswer(wire.OpStallBuyResult, code)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpStallBuyResult, Payload: wire.EncodeStallResult(0, slot)}}}
}

/*
================
sellStallSlot

Moves a slot's count to the buyer and its price, less commission, to the
owner, then tells the stall (0x3260 kind 3 or 4) and both players' gold.
expectSerial, when not zero, is the network row the buyer saw.
================
*/
func (rt *Runtime) sellStallSlot(division, ownerName string, slot uint8, buyer *enterworld.Character, kind uint8, expectSerial uint64) uint8 {
	owner := rt.findCharacter(division, ownerName)
	s, ok := rt.Stalls.Get(division, ownerName)
	if owner == nil || !ok {
		return wire.StallErrHostLeft
	}
	if !s.Open {
		return wire.StallErrMarketClosed
	}
	if int(slot) >= stall.Slots || s.Slots[slot] == nil {
		return wire.StallErrBadSlot
	}
	offer := *s.Slots[slot]
	if expectSerial != 0 && offer.Serial != expectSerial {
		return wire.StallErrNetworkStale
	}
	commission := uint64(0)
	if kind == wire.StallEventNetworkSold {
		commission = stallCommission(offer.Price)
	}
	code := uint8(0)
	sold := rt.deps.UpdateMany([]*enterworld.Character{owner, buyer}, "stall-sale", func() bool {
		if goldOf(buyer) < uint64(offer.Price) {
			code = wire.StallErrNotEnoughGold
			return false
		}
		ownerBag := bagOf(owner)
		buyerBag := bagOf(buyer)
		if _, free := buyerBag.FirstFreeBagSlot(); !free {
			code = wire.StallErrInventoryFull
			return false
		}
		item, present := ownerBag.At(offer.BagSlot)
		if !present || item.RefObjID != offer.RefObjID {
			code = wire.StallErrBadSlot
			return false
		}
		taken, fault := ownerBag.DropQuantity(offer.BagSlot, offer.Quantity)
		if fault != nil {
			code = wire.StallErrBadCount
			return false
		}
		if _, fault := buyerBag.Grant(taken); fault != nil {
			code = wire.StallErrInventoryFull
			return false
		}
		owner.MissionInventory = rowsFromInvItems(ownerBag.Items())
		buyer.MissionInventory = rowsFromInvItems(buyerBag.Items())
		setGold(buyer, goldOf(buyer)-uint64(offer.Price))
		setGold(owner, goldOf(owner)+uint64(offer.Price)-commission)
		return true
	})
	if !sold {
		if code == 0 {
			code = wire.StallErrInvalidOp
		}
		if kind == wire.StallEventNetworkSold {
			switch code {
			case wire.StallErrNotEnoughGold:
				code = wire.StallErrNetworkGold
			case wire.StallErrInventoryFull:
				code = wire.StallErrNetworkBag
			}
		}
		return code
	}
	after, _ := rt.Stalls.Edit(division, ownerName, func(st *stall.Stall) error {
		st.Slots[slot] = nil
		return nil
	})
	event := wire.Frame{Opcode: wire.OpStallEvent, Payload: wire.EncodeStallSold(kind, slot, buyer.Name, rt.stallOffers(division, owner, after))}
	rt.pushStall(division, after, "", event)
	rt.PushCharacterFrames(division, owner.Name, []wire.Frame{goldFrame(rt.characterSnapshot(division, owner))})
	rt.PushCharacterFrames(division, buyer.Name, []wire.Frame{goldFrame(rt.characterSnapshot(division, buyer))})
	return 0
}

/*
================
stallCommission

The stall network's 1% (UIIT_MSG_WARENETWORK_REGIST_03), clamped to
[1, 100000] as 755960 prints it.
================
*/
func stallCommission(price uint32) uint64 {
	fee := uint64(price) / 100
	if fee < 1 {
		fee = 1
	}
	if fee > 100000 {
		fee = 100000
	}
	if fee > uint64(price) {
		fee = uint64(price)
	}
	return fee
}
