/*
===========================================================================

commerce.go - NPC shop projections and authoritative gold transactions

Package decoding and delivery are shared with the mall. Selected NPC, tax,
buyback and gold policy stay in this merchant transaction owner.

===========================================================================
*/
package action

import (
	"encoding/json"
	"errors"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"reflect"
	"strconv"
)

// Browser extensions, not retail opcodes. Prices and item identities never
// come from the request: only the selected merchant, tab, slot and quantity do.
const opShopCatalog uint16 = 11
const opShopInventory uint16 = 12

/*
================
ConfigureCommerce
================
*/
func (rt *Runtime) ConfigureCommerce(dir string) error {
	c, e := commerce.Load(dir, rt.deps.ItemReferences())
	if e == nil {
		rt.Commerce = c
		rt.prepareCommerceReferences()
	}
	return e
}

/*
================
shopContent
================
*/
type shopContent struct {
	Ref      uint32 `json:"refObjId"`
	Name     string `json:"name"`
	Quantity uint32 `json:"quantity"`
	Plus     uint8  `json:"plus"`
}

/*
================
shopOffer
================
*/
type shopOffer struct {
	PurchaseLimit uint16             `json:"purchaseLimit"`
	Previews      []shopInventoryRow `json:"previews"`
	Contents      []shopContent      `json:"contents,omitempty"`
	Tab           uint8              `json:"tab"`
	Slot          uint8              `json:"slot"`
	Ref           uint32             `json:"refObjId"`
	Name          string             `json:"name"`
	Price         string             `json:"price"`
	Stack         uint16             `json:"maxStack"`
}

/*
================
shopTabPresentation
================
*/
type shopTabPresentation struct {
	Index       uint8  `json:"index"`
	LabelSymbol string `json:"labelSymbol"`
}

/*
================
shopProjection
================
*/
type shopProjection struct {
	SaleQuotes []shopSaleQuote       `json:"saleQuotes"`
	Tabs       []shopTabPresentation `json:"tabs"`
	Version    int                   `json:"version"`
	Npc        uint32                `json:"npc"`
	Name       string                `json:"name"`
	Offers     []shopOffer           `json:"offers"`
	Buyback    []buybackOffer        `json:"buyback"`
	Error      string                `json:"error,omitempty"`
}

/*
================
commerceNpc
================
*/
func (rt *Runtime) commerceNpc(division string, c *enterworld.Character, gid uint32) (simulation.NpcDef, bool) {
	if c == nil || rt.Commerce == nil {
		return simulation.NpcDef{}, false
	}
	selected, ok := rt.Selected.Get(division, c.Name)
	if !ok || selected != gid {
		return simulation.NpcDef{}, false
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || npc.TalkFlags&simulation.NpcTalkFlagShop == 0 || !npc.AuthoredSpawn || npc.Patrol {
		return npc, false
	}
	live := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())
	distance := simulation.WorldDistance2D(live, npc.Spawn)
	// Explicit rebuild service policy, not an original-server distance proof.
	return npc, !math.IsNaN(distance) && !math.IsInf(distance, 0) && distance <= 150
}

/*
================
eachShopOffer
================
*/
func (rt *Runtime) eachShopOffer(npc simulation.NpcDef, visit func(uint8, commerce.Offer)) {
	index := 0
	for _, group := range npc.NpcTalkStoreGroups {
		for _, tab := range group.Tabs {
			if index > 255 {
				return
			}
			for _, offer := range rt.Commerce.Tabs[tab.TabID] {
				visit(uint8(index), offer)
			}
			index++
		}
	}
}

/*
================
shopCatalog
================
*/
func (rt *Runtime) shopCatalog(division string, c *enterworld.Character, gid uint32) wire.Frame {
	p := shopProjection{Version: 1, Npc: gid, Offers: []shopOffer{}, Tabs: []shopTabPresentation{}}
	snapshot := rt.characterSnapshot(division, c)
	npc, ok := rt.commerceNpc(division, snapshot, gid)
	if !ok {
		p.Error = "Select a nearby merchant"
	} else {
		p.Name = npc.Name
		for _, group := range npc.NpcTalkStoreGroups {
			for _, tab := range group.Tabs {
				if len(p.Tabs) < 256 {
					p.Tabs = append(p.Tabs, shopTabPresentation{Index: uint8(len(p.Tabs)), LabelSymbol: tab.LabelSymbol})
				}
			}
		}
		p.Buyback = rt.buybackOffers(rt.characterSnapshot(division, c), npc.RefObjID)
		p.SaleQuotes = rt.shopSaleQuotes(division, snapshot, npc.RefObjID)
		tax := rt.commerceTax(division, npc.RefObjID, snapshot)
		rt.eachShopOffer(npc, func(tab uint8, o commerce.Offer) {
			price, valid := commerce.AdjustPrice(o.Price, tax, true)
			if !valid {
				return
			}
			contents := []shopContent{}
			previews := []shopInventoryRow{}
			templates := o.Contents
			if len(templates) == 0 {
				templates = []commerce.Content{{Ref: o.Ref, Stack: o.Stack}}
			}
			for _, entry := range templates {
				quantity := uint32(1)
				if inventory.IsEtcStackableTypeFlags(entry.Ref.TypeFlags()) && entry.Data > 0 {
					quantity = entry.Data
				}
				contents = append(contents, shopContent{entry.Ref.RefObjID, entry.Ref.Name, quantity, entry.Plus})
				item := inventory.Item{RefObjID: entry.Ref.RefObjID, Codename: entry.Ref.Codename, TypeFlags: entry.Ref.TypeFlags(), Plus: entry.Plus, VarianceBits: entry.Variance, Durability: entry.Data, MagicOptions: append([]uint64(nil), entry.Magic...), Quantity: uint16(quantity)}
				rows, err := rt.shopInventoryRows([]inventory.Item{item}, nil)
				if err != nil {
					return
				}
				previews = append(previews, rows...)
			}
			// v1.150 6C0540: package count differs from item stack capacity.
			purchaseLimit := uint16(5)
			if len(templates) == 1 && inventory.IsEtcStackableTypeFlags(templates[0].Ref.TypeFlags()) && templates[0].Data == 0 {
				purchaseLimit = templates[0].Stack
			}
			p.Offers = append(p.Offers, shopOffer{PurchaseLimit: purchaseLimit, Previews: previews, Contents: contents, Tab: tab, Slot: o.Slot, Ref: o.Ref.RefObjID, Name: o.Ref.Name, Price: strconv.FormatUint(price, 10), Stack: o.Stack})
		})
	}
	b, _ := json.Marshal(p)
	return wire.Frame{Opcode: opShopCatalog, Payload: b}
}

/*
================
applyCommerce
================
*/
func (rt *Runtime) applyCommerce(division string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	snapshot := rt.characterSnapshot(division, c)
	npc, ok := rt.commerceNpc(division, snapshot, q.NpcGID)
	if !ok {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	nativeType := q.MovementType
	isCOS := nativeType == wire.MoveTypeCosShopBuy || nativeType == wire.MoveTypeCosShopSell
	if isCOS {
		if nativeType == wire.MoveTypeCosShopBuy {
			q.MovementType = wire.MoveTypeShopBuy
		} else {
			q.MovementType = wire.MoveTypeShopSell
		}
	}
	result := failureResult(wire.ErrCodeInvalidRequest)
	committed := rt.deps.Update(c, "shop-transaction", func() bool {
		if c.DeletePending {
			return false
		}
		before := invItemsFromRows(c.MissionInventory)
		inv := inventory.New(before)
		var container *domain.COSContainer
		if isCOS {
			var valid bool
			container, inv, valid = rt.ownedCOSContainer(c, q.CosGID)
			if !valid {
				return false
			}
			before = inv.Items()
		}

		balance := goldOf(c)
		var response []byte
		buyback, nextID := c.Buyback, c.BuybackNext
		switch q.MovementType {
		case wire.MoveTypeShopBuy:
			var offer *commerce.Offer
			rt.eachShopOffer(npc, func(tab uint8, o commerce.Offer) {
				if tab == q.ShopTab && o.Slot == q.ShopSlot {
					copy := o
					offer = &copy
				}
			})
			if offer == nil || q.Quantity == 0 {
				return false
			}
			unit, valid := commerce.AdjustPrice(offer.Price, rt.commerceTax(division, npc.RefObjID, c), true)
			if !valid || unit > math.MaxInt64/uint64(q.Quantity) {
				return false
			}
			cost := unit * uint64(q.Quantity)
			if cost > balance {
				result = failureResult(wire.ErrCodeNotEnoughGold)
				return false
			}
			contents := offer.Contents
			if len(contents) == 0 {
				contents = []commerce.Content{{Ref: offer.Ref, Stack: offer.Stack}}
			}
			capacity := uint16(inventory.BagSlotEnd - inventory.EquipmentSlotEnd)
			if container != nil {
				capacity = uint16(container.Capacity)
			}
			dest, grantErr := commerce.GrantPackage(inv, contents, q.Quantity, capacity)
			if grantErr != nil {
				result = commerceFailure(grantErr)
				return false
			}
			balance -= cost
			w := wire.NewWriter(7 + len(dest)).U8(1).U8(8).U8(q.ShopTab).U8(q.ShopSlot).U8(uint8(len(dest)))
			for _, slot := range dest {
				w.U8(slot)
			}
			response = w.U16(q.Quantity).Payload()
		case wire.MoveTypeShopSell:
			item, found := inv.At(q.SourceSlot)
			if !found || !isCOS && q.SourceSlot < inventory.EquipmentSlotEnd {
				return false
			}
			ref, found := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
			if !found || ref == nil || ref.RefObjID != item.RefObjID || ref.TypeFlags() != item.TypeFlags {
				return false
			}
			admitted := commerceSaleAdmitted(ref)
			if !admitted || !inventory.IsEtcStackableTypeFlags(item.TypeFlags) && q.Quantity != 1 {
				return false
			}
			price, restore, priced := commerce.SalePrices(ref, item.MagicOptions, rt.Commerce.Magic, rt.commerceTax(division, npc.RefObjID, c))
			if !priced {
				return false
			}
			if q.Quantity == 0 || price > math.MaxInt64/uint64(q.Quantity) || restore > math.MaxInt64/uint64(q.Quantity) {
				return false
			}
			credit := price * uint64(q.Quantity)
			if credit > math.MaxInt64-balance {
				return false
			}
			if _, fault := inv.DropQuantity(q.SourceSlot, q.Quantity); fault != nil {
				result = commerceFailure(fault)
				return false
			}
			if !commerceNoBuyback(item.TypeFlags, item.Codename) {
				var retained bool
				buyback, nextID, retained = retainSale(c, npc.RefObjID, item, q.Quantity, restore*uint64(q.Quantity))
				if !retained {
					return false
				}
			}
			balance += credit
			response = wire.NewWriter(10).U8(1).U8(9).U8(q.SourceSlot).U16(q.Quantity).U32(q.NpcGID).U8(uint8(len(buyback) - 1)).Payload()
		default:
			return false
		}
		frames := []wire.Frame{}
		broadcasts := []wire.Frame{}
		if q.MovementType == wire.MoveTypeShopBuy {
			frame, e := rt.shopInventory(inv.Items(), before, q)
			if e != nil {
				return false
			}
			reference := rt.commerceReferences(inv.Items(), before)
			frames = append(frames, reference, frame)
			broadcasts = append(broadcasts, reference)
		}
		if isCOS {
			response = append(wire.NewWriter(6).U8(1).U8(nativeType).U32(q.CosGID).Payload(), response[2:]...)
		}
		frames = append(frames, wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: response}, wire.Frame{Opcode: wire.OpGoldRefresh, Payload: wire.GoldRefresh{Balance: balance}.Encode()})
		if isCOS {
			container.Rows = rowsFromInvItems(inv.Items())
		} else {
			c.MissionInventory = rowsFromInvItems(inv.Items())
		}
		setGold(c, balance)
		c.Buyback, c.BuybackNext = buyback, nextID
		if q.MovementType == wire.MoveTypeShopSell {
			frames = append(frames, rt.buybackFrame(c, q.NpcGID, npc.RefObjID, 0, nil, ""))
		}
		result = OpResult{Frames: frames, Broadcast: broadcasts}
		return true
	})
	if !committed {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	return result
}

/*
================
shopInventoryRow
================
*/
type shopInventoryRow struct {
	Slot uint8  `json:"slot"`
	Ref  uint32 `json:"refObjId"`
	Type uint16 `json:"typeFlags"`
	Name string `json:"name"`
	Body []int  `json:"body"`
}

/*
================
shopInventoryRows
================
*/
func (rt *Runtime) shopInventoryRows(items []inventory.Item, before []inventory.Item) ([]shopInventoryRow, error) {
	rows := []shopInventoryRow{}
	for _, item := range items {
		unchanged := false
		for _, old := range before {
			if old.Slot == item.Slot && reflect.DeepEqual(old, item) {
				unchanged = true
				break
			}
		}
		if unchanged {
			continue
		}
		body := item.Body().Encode()
		bytes := make([]int, len(body))
		for i, b := range body {
			bytes[i] = int(b)
		}
		name := item.Codename
		if ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename); ok && ref != nil {
			name = ref.Name
		}
		rows = append(rows, shopInventoryRow{item.Slot, item.RefObjID, item.TypeFlags, name, bytes})
	}
	return rows, nil
}

/*
================
shopInventory
================
*/
func (rt *Runtime) shopInventory(items []inventory.Item, before []inventory.Item, q wire.ItemMoveRequest) (wire.Frame, error) {
	rows, e := rt.shopInventoryRows(items, before)
	if e != nil {
		return wire.Frame{}, e
	}
	payload, e := json.Marshal(struct {
		CosGID   uint32             `json:"cosGid,omitempty"`
		Version  int                `json:"version"`
		Npc      uint32             `json:"npc"`
		Tab      uint8              `json:"tab"`
		Slot     uint8              `json:"slot"`
		Quantity uint16             `json:"quantity"`
		Items    []shopInventoryRow `json:"items"`
	}{q.CosGID, 1, q.NpcGID, q.ShopTab, q.ShopSlot, q.Quantity, rows})
	return wire.Frame{Opcode: opShopInventory, Payload: payload}, e
}

/*
================
commerceFailure

Package placement and currency refusals keep their native UI categories.
Other failures disclose no storage details to the requesting client.
================
*/
func commerceFailure(err error) OpResult {
	var fault *inventory.Fault
	if errors.As(err, &fault) {
		return failureResult(fault.Code)
	}
	var insufficient domain.MallInsufficientCurrency
	if errors.As(err, &insufficient) {
		return failureResult(wire.ErrCodeMallInsufficientCurrency)
	}
	return failureResult(wire.ErrCodeInvalidRequest)
}
