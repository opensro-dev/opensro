/*
===========================================================================

itemmall.go - native Item Mall admission, catalogue and durable purchases

Transport resolves the authenticated character. Catalogue prices and package
contents are server-owned; the store commits currency and inventory together.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/transport"
)

// Browser reference projection; retail purchases still use 706D type 18.
const opMallCatalog uint16 = 15

// Browser silk-history inquiry (port-only, #2269). The native window
// ships in v1.150 with its Inquire button wired to an empty stub and no
// packet ever answers it, so the inquiry rides the replacement
// transport's control range like the catalogue does. The period is the
// native combo's index: 1 day, 7 days, 1 month, 3 months.
const opMallSilkHistory uint16 = 18

var mallSilkPeriodDays = [...]int{1, 7, 30, 90}

// The most uses one answer lists; the period total is never capped.
const mallSilkHistoryLimit = 100

/*
================
mallSilkHistoryRequest
================
*/
type mallSilkHistoryRequest struct {
	Period int `json:"period"`
}

/*
================
mallSilkHistoryEntry

One recorded use on the wire: exactly at and charged, the key set every
shipped client's decoder pins. The earned part of the charge stays in
the store's split (#4133); it does not ride this frame.
================
*/
type mallSilkHistoryEntry struct {
	At      int64  `json:"at"`
	Charged uint32 `json:"charged"`
}

/*
================
mallSilkHistoryProjection
================
*/
type mallSilkHistoryProjection struct {
	Version int                    `json:"version"`
	Period  int                    `json:"period"`
	Total   uint32                 `json:"total"`
	Rows    []mallSilkHistoryEntry `json:"rows"`
}

/*
================
mallProjection
================
*/
type mallProjection struct {
	Version int `json:"version"`
	domain.MallBalance
	// EarnedTradable tells the mall whether a purchase paid wholly
	// from earned silk comes out tradable (#4133), so its confirm can
	// say which way this buy lands before the player commits.
	EarnedTradable bool                   `json:"earnedTradable"`
	Tabs           []commerce.MallTab     `json:"tabs"`
	Offers         []commerce.MallPackage `json:"offers"`
	Items          []shopInventoryRow     `json:"items,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

/*
================
ConfigureMall
================
*/
func (rt *Runtime) ConfigureMall(dir string, authority domain.MallAuthority, extra []commerce.MallExtraGood,
	categories bool) error {
	if authority == nil {
		return fmt.Errorf("mall: missing durable currency authority")
	}
	catalog, err := commerce.LoadMall(dir, extra, categories, rt.deps.ItemReferences())
	if err != nil {
		return err
	}
	if err := catalog.Hydrate(rt.deps.ItemReferences()); err != nil {
		return err
	}
	rt.withholdUniqueOnlyGoods(catalog)
	rt.mallCatalog, rt.mallAuthority = catalog, authority
	return nil
}

/*
================
withholdUniqueOnlyGoods

Port-only, not native (owner 2026-11-25): goods a unique kill guarantees
or rolls come from uniques only, so the mall stops selling every package
that holds one. While SRO_UNIQUE_POP_TICKET adds a Magic Pop card to each
unique kill the card leaves the mall; while the Devil's Spirit lucky drops
run (NasrunTokens, nasrun_drops.go) their two prizes leave it: every
Spirit, both sexes and colours, and Sabakun's Jewel. The token shop keeps
selling Spirits: its tokens are unique rewards too. Both off, the mall is
the one the built tree carries. The wiring sets
UniqueRules before the mall loads (wiring_gameplay.go).
================
*/
func (rt *Runtime) withholdUniqueOnlyGoods(catalog *commerce.MallCatalog) {
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return
	}
	var card uint32
	if rt.UniqueRules.PopTicket {
		if ref, ok := refs.ItemRefByCodename(gacha.CardCodename); ok && ref != nil {
			card = ref.RefObjID
		}
	}
	spirits := rt.NasrunTokens
	if card == 0 && !spirits {
		return
	}
	removed := catalog.WithholdPackagesHolding(func(refObjID uint32) bool {
		if card != 0 && refObjID == card {
			return true
		}
		if !spirits {
			return false
		}
		ref, ok := refs.ItemRefByID(refObjID)
		return ok && ref != nil && (nasrun.IsSpirit(ref.TypeFlags()) || ref.Codename == nasrunDropJewel)
	})
	if removed > 0 {
		log.Infof("mall: %d unique-only offer(s) withheld (Magic Pop card %t, Devil's Spirit %t; port-only)",
			removed, card != 0, spirits)
	}
}

/*
================
registerMall
================
*/
func (rt *Runtime) registerMall(hub *transport.Hub) {
	hub.Handle(opMallCatalog, rt.hubHandler(hub, rt.HandleMallCatalog))
	hub.Handle(opMallSilkHistory, rt.hubHandler(hub, rt.HandleMallSilkHistory))
}

/*
================
mallFrame
================
*/
func (rt *Runtime) mallFrame(balance domain.MallBalance, items []shopInventoryRow) (wire.Frame, error) {
	payload, err := json.Marshal(mallProjection{Version: 1, MallBalance: balance, EarnedTradable: rt.MallEarnedTradable, Tabs: rt.mallCatalog.Tabs, Offers: rt.mallCatalog.Offers, Items: items})
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
HandleMallSilkHistory

The window's period is the request's only input; the account is the
session character's own, exactly as the catalogue's. Uses are listed
newest first with the period's total charge beside them.
================
*/
func (rt *Runtime) HandleMallSilkHistory(division string, character *enterworld.Character, payload []byte) OpResult {
	if character == nil || rt.mallAuthority == nil || len(payload) == 0 || len(payload) > 64 {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request mallSilkHistoryRequest
	if err := decoder.Decode(&request); err != nil || request.Period < 0 || request.Period >= len(mallSilkPeriodDays) {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, character)
	if snapshot == nil || snapshot.DeletePending {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	rows, total, err := rt.mallAuthority.SilkHistory(character, mallSilkPeriodDays[request.Period], mallSilkHistoryLimit)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	projection := mallSilkHistoryProjection{Version: 1, Period: request.Period, Total: total, Rows: []mallSilkHistoryEntry{}}
	for _, row := range rows {
		projection.Rows = append(projection.Rows, mallSilkHistoryEntry{At: row.AtMs / 1000, Charged: row.Charged})
	}
	answer, err := json.Marshal(projection)
	if err != nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opMallSilkHistory, Payload: answer}}}
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
	balance, err := rt.mallAuthority.PurchaseMall(character, quote.Cost, func(rows []domain.InventoryRow, earnedOnly bool) ([]domain.InventoryRow, error) {
		// The one delivery decision (#4133): what paid is stamped on
		// the items - the earned mark records the payment, the seal
		// carries the tradable rule. Earned silk alone paid and the
		// rule on delivers unsealed and marked (tradable); anything
		// else lands sealed. Every door reads the outcome from the
		// item, through the one predicate (Runtime.Tradeable) or the
		// seal.
		bound := !earnedOnly || !rt.MallEarnedTradable
		bagEnd := inventory.BagEnd(character)
		before := invItemsFromRowsWithin(rows, int64(bagEnd))
		inv := inventory.New(before, bagEnd)
		destinations, err := commerce.GrantPackage(inv, quote.Offer.Contents, quote.Quantity, uint16(bagEnd-inventory.EquipmentSlotEnd), commerce.GrantMarks{Bound: bound, EarnedBought: earnedOnly, NowUnix: rt.Now().Unix()})
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
