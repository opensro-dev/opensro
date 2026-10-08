/*
===========================================================================

itemmall.go - native Item Mall admission, catalogue and durable purchases

Transport resolves the authenticated character. Catalogue prices and package
contents are server-owned; the store commits currency and inventory together.

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// Browser reference projection; retail purchases still use 706D type 18.
const opMallCatalog uint16 = 15

/*
================
mallProjection
================
*/
type mallProjection struct {
	Version int `json:"version"`
	domain.MallBalance
	Tabs   []commerce.MallTab     `json:"tabs"`
	Offers []commerce.MallPackage `json:"offers"`
	Items  []shopInventoryRow     `json:"items,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

/*
================
ConfigureMall
================
*/
func (rt *Runtime) ConfigureMall(dir string, authority domain.MallAuthority) error {
	if authority == nil {
		return fmt.Errorf("mall: missing durable currency authority")
	}
	catalog, err := commerce.LoadMall(dir)
	if err != nil {
		return err
	}
	if err := catalog.Hydrate(rt.deps.ItemReferences()); err != nil {
		return err
	}
	rt.mallCatalog, rt.mallAuthority = catalog, authority
	return nil
}

/*
================
registerMall
================
*/
func (rt *Runtime) registerMall(hub *transport.Hub) {
	hub.Handle(opMallCatalog, rt.hubHandler(hub, rt.HandleMallCatalog))
}

/*
================
mallFrame
================
*/
func (rt *Runtime) mallFrame(balance domain.MallBalance, items []shopInventoryRow) (wire.Frame, error) {
	payload, err := json.Marshal(mallProjection{Version: 1, MallBalance: balance, Tabs: rt.mallCatalog.Tabs, Offers: rt.mallCatalog.Offers, Items: items})
	return wire.Frame{Opcode: opMallCatalog, Payload: payload}, err
}

/*
================
HandleMallCatalog

An empty request uses only the session-bound character. No account identifier
or target character is accepted from the browser.
================
*/
func (rt *Runtime) HandleMallCatalog(division string, character *enterworld.Character, payload []byte) OpResult {
	if len(payload) != 0 || character == nil || rt.mallCatalog == nil || rt.mallAuthority == nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, character)
	if snapshot == nil || snapshot.DeletePending {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	balance, err := rt.mallAuthority.MallBalance(character)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	frame, err := rt.mallFrame(balance, nil)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	return OpResult{Frames: []wire.Frame{frame}}
}

/*
================
HandleMallPurchase

Prepare all fallible delivery projections inside the store transaction's
callback. A failed debit or failed commit must never emit a success packet.
================
*/
func (rt *Runtime) HandleMallPurchase(division string, character *enterworld.Character, payload []byte) OpResult {
	request, err := wire.DecodeMallPurchase(payload)
	if err != nil || character == nil || rt.mallCatalog == nil || rt.mallAuthority == nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, character)
	if snapshot == nil || snapshot.DeletePending {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	quote, err := rt.mallCatalog.Quote(request)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	rt.Pending.Clear(grounditem.PendingKey(division, character.Name))
	var delivery []shopInventoryRow
	var response []byte
	var reference wire.Frame
	balance, err := rt.mallAuthority.PurchaseMall(character, quote.Cost, func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
		bagEnd := inventory.BagEnd(character)
		before := invItemsFromRowsWithin(rows, int64(bagEnd))
		inv := inventory.New(before, bagEnd)
		destinations, err := commerce.GrantPackage(inv, quote.Offer.Contents, quote.Quantity, uint16(bagEnd-inventory.EquipmentSlotEnd))
		if err != nil {
			return nil, err
		}
		response, err = wire.EncodeMallPurchaseResult(request, destinations)
		if err != nil {
			return nil, err
		}
		after := inv.Items()
		delivery, err = rt.shopInventoryRows(after, before)
		if err != nil {
			return nil, err
		}
		reference = rt.commerceReferences(after, before)
		return rowsFromInvItems(after), nil
	})
	if err != nil {
		return commerceFailure(err)
	}
	frame, err := rt.mallFrame(balance, delivery)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	return OpResult{Frames: []wire.Frame{reference, frame, {Opcode: wire.OpItemMoveResponse, Payload: response}}, Broadcast: []wire.Frame{reference}}
}
