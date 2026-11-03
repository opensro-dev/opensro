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
	"fmt"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
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

The shipped catalogue plus the operator's extra goods (port-only, not
native; commerce.AddExtraGoods). Nil extra is the v1.150 stock.
================
*/
func (rt *Runtime) ConfigureCommerce(dir string, extra []commerce.ExtraGood) error {
	c, e := commerce.LoadWithExtraGoods(dir, rt.deps.ItemReferences(), extra)
	if e != nil {
		return e
	}
	// Port-only, not native, and permanent (owner 2026-10-20): no NPC
	// sells degree-9 Sun gear, whatever SRO_SUN_EVENT_ONLY says. That
	// flag governed drops and shops together until the 2026-10-20 flip
	// made drops the Sun source; the shop half was deliberately kept, so
	// Sun supply is drop-side only, forever. Do not make this conditional
	// again - withholding here is the rule, not a leftover of the gate.
	c.WithholdGoods(func(ref *enterworld.ItemRef) bool { return SunShopExcluded(ref) })
	if rt.TradeTiers {
		// Port-only, not native: the shops price trade goods with their
		// scaled references (SRO_TRADE_TIERS).
		c.ScaleTradeGoodsPrices(enterworld.TradeGoodsPriceFactor)
	}
	rt.Commerce = c
	rt.tradeOrigins = tradeOriginTowns(c)
	rt.prepareCommerceReferences()
	return nil
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
shopPriceRow
================
*/
type shopPriceRow struct {
	Currency uint8  `json:"currency"`
	Amount   string `json:"amount"`
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
	// Currency is the refpricepolicyofitem payment type: 1 gold, 32 honor.
	Currency uint8          `json:"currency"`
	Prices   []shopPriceRow `json:"prices,omitempty"`
	Stack    uint16         `json:"maxStack"`
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
	CosGID     uint32                `json:"cosGid,omitempty"`
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

The merchant a trade may use: the selected, live, authored shop NPC whose
shop function an in-range request opened (npcrange.go). Native trades check
that function state (the player's +0xC+6 == 5), never distance, so a shop that
opened validly keeps trading until the selection is released or replaced.
================
*/
func (rt *Runtime) commerceNpc(division string, c *enterworld.Character, gid uint32) (simulation.NpcDef, bool) {
	if c == nil || rt.Commerce == nil {
		return simulation.NpcDef{}, false
	}
	if !rt.Selected.FunctionOpen(division, c.Name, gid) {
		return simulation.NpcDef{}, false
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || npc.TalkFlags&(simulation.NpcTalkFlagShop|simulation.NpcTalkFlagSpecialTrade) == 0 || !npc.AuthoredSpawn || npc.Patrol {
		return npc, false
	}
	return npc, true
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
				if rt.AttackPetExpansion && petGearShopOffer(offer) {
					continue
				}
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
		if npc.TalkFlags&simulation.NpcTalkFlagSpecialTrade != 0 && snapshot.ActiveCOS != nil && snapshot.ActiveCOS.Summoned {
			p.CosGID = snapshot.ActiveCOS.GID
		}
		for _, group := range npc.NpcTalkStoreGroups {
			for _, tab := range group.Tabs {
				if len(p.Tabs) < 256 {
					p.Tabs = append(p.Tabs, shopTabPresentation{Index: uint8(len(p.Tabs)), LabelSymbol: tab.LabelSymbol})
				}
			}
		}
		p.Buyback = rt.buybackOffers(rt.characterSnapshot(division, c), npc.RefObjID)
		p.SaleQuotes = rt.shopSaleQuotes(division, snapshot, npc)
		tax := rt.commerceTax(division, npc, snapshot)
		rt.eachShopOffer(npc, func(tab uint8, o commerce.Offer) {
			price, valid := offerUnitPrice(o, tax)
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
			var prices []shopPriceRow
			if o.Currency == commerce.PaymentGold && (o.GuildPointPrice > 0 || o.HonorPrice > 0) {
				prices = append(prices, shopPriceRow{Currency: commerce.PaymentGold, Amount: strconv.FormatUint(price, 10)})
				if o.GuildPointPrice > 0 {
					prices = append(prices, shopPriceRow{Currency: commerce.PaymentGuildPoint, Amount: strconv.FormatUint(o.GuildPointPrice, 10)})
				}
				if o.HonorPrice > 0 {
					prices = append(prices, shopPriceRow{Currency: commerce.PaymentHonor, Amount: strconv.FormatUint(o.HonorPrice, 10)})
				}
			}
			p.Offers = append(p.Offers, shopOffer{PurchaseLimit: purchaseLimit, Previews: previews, Contents: contents, Tab: tab, Slot: o.Slot, Ref: o.Ref.RefObjID, Name: o.Ref.Name, Price: strconv.FormatUint(price, 10), Currency: o.Currency, Prices: prices, Stack: o.Stack})
		})
	}
	b, _ := json.Marshal(p)
	return wire.Frame{Opcode: opShopCatalog, Payload: b}
}

/*
================
commerceDoorLabel

The door a shop transaction commits under, which names its gold rows in
the economy ledger (port-only, domain/economy.go): a sale whose sold row is
trade goods (in the bag, or in the COS bag the sale names, a transport's
or a pickup pet's) pays out under "trade-sale <seller id>", or under
"thief-sale <seller id>" when a thief sells stolen goods at the den; the
id lets the ledger tell the seller's credit from the party's shares. Every
other purchase or sale stays "shop-transaction". The snapshot is read
before the door; the door's own admission still decides.
================
*/
func commerceDoorLabel(snapshot *enterworld.Character, q wire.ItemMoveRequest, fromCOS bool) string {
	if q.MovementType != wire.MoveTypeShopSell {
		return "shop-transaction"
	}
	rows := snapshot.MissionInventory
	if fromCOS {
		rows = nil
		if cos := snapshot.CompanionByGID(q.CosGID); cos != nil && cos.Container != nil {
			rows = cos.Container.Rows
		}
	}
	for _, row := range rows {
		if row.Slot != int64(q.SourceSlot) || !inventory.IsTradeGoods(row.TypeFlags) {
			continue
		}
		if enterworld.DressedJob(snapshot) == domain.JobThief {
			return fmt.Sprintf("thief-sale %d", snapshot.ID)
		}
		return fmt.Sprintf("trade-sale %d", snapshot.ID)
	}
	return "shop-transaction"
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
	// refusal names the native cause of an uncommitted trade; the silent
	// generic code covers requests the retail client cannot compose.
	refusal := wire.ErrCodeInvalidRequest
	if q.MovementType == wire.MoveTypeShopBuy {
		rt.honorMu.Lock()
		defer rt.honorMu.Unlock()
	}
	roster := rt.commerceRoster(division, c)
	availableHonor := rt.honorPoints(division, snapshot)
	isGuildMaster, availableGuildPoints := rt.guildShopAdmission(division, snapshot)
	var collected int64
	var guildPointCost uint64
	var honorCost uint64
	committed := rt.deps.SettleTrade(roster.members, commerceDoorLabel(snapshot, q, isCOS), func(pool *domain.TradeRewardPool) bool {
		if c.DeletePending {
			return false
		}
		before := invItemsFromBag(c)
		inv := inventory.New(before, inventory.BagEnd(c))
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
		var payouts []tradePayout
		var weeklyCredit uint64
		// collected is the fortress tax this trade paid (the taxed price
		// minus the untaxed one): the purchase charge or the trade
		// goods sale. An ordinary item sale collects nothing; no sale path
		// in the original accrues fortress tax.
		collected = 0
		guildPointCost = 0
		honorCost = 0
		settledPool := *pool
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
			unit, valid := offerUnitPrice(*offer, rt.commerceTax(division, npc, c))
			if !valid || unit > math.MaxInt64/uint64(q.Quantity) {
				return false
			}
			cost := unit * uint64(q.Quantity)
			if offer.Currency == 0 || offer.Currency == commerce.PaymentGold {
				collected = int64(cost) - int64(offer.Price*uint64(q.Quantity))
			}
			switch offer.Currency {
			case commerce.PaymentHonor:
				if cost > math.MaxUint32 || cost > availableHonor {
					refusal = wire.ErrCodeNotEnoughHonor
					return false
				}
				honorCost = cost
				// Paid in honor: the gold balance is untouched.
				cost = 0
			case commerce.PaymentGuildPoint:
				if !isGuildMaster {
					refusal = wire.ErrCodeGuildShopNotMaster
					return false
				}
				if cost > math.MaxUint32 || cost > availableGuildPoints {
					refusal = wire.ErrCodeNotEnoughGuildPoints
					return false
				}
				guildPointCost = cost
				cost = 0
			default:
				if cost > balance {
					refusal = wire.ErrCodeNotEnoughGold
					return false
				}
				if offer.HonorPrice > 0 {
					if offer.HonorPrice > math.MaxUint32/uint64(q.Quantity) {
						return false
					}
					honorCost = offer.HonorPrice * uint64(q.Quantity)
					if honorCost > availableHonor {
						refusal = wire.ErrCodeNotEnoughHonor
						return false
					}
				}
				if offer.GuildPointPrice > 0 {
					if !isGuildMaster {
						refusal = wire.ErrCodeGuildShopNotMaster
						return false
					}
					if offer.GuildPointPrice > math.MaxUint32/uint64(q.Quantity) {
						return false
					}
					guildPointCost = offer.GuildPointPrice * uint64(q.Quantity)
					if guildPointCost > availableGuildPoints {
						refusal = wire.ErrCodeNotEnoughGuildPoints
						return false
					}
				}
			}
			contents := append([]commerce.Content(nil), offer.Contents...)
			if len(contents) == 0 {
				contents = []commerce.Content{{Ref: offer.Ref, Stack: offer.Stack}}
			}
			// INFERENCE from the native item owner checks: seed purchased goods
			// with the buyer's original job alias,
			// including when another holder later picks them up. Set it before
			// GrantPackage chooses a merge destination, on a private template.
			for index := range contents {
				if contents[index].Ref != nil && inventory.IsTradeGoods(contents[index].Ref.TypeFlags()) {
					contents[index].TradeOwner = c.Job.Alias
					// Natively a suited trader with a transport out buys.
					if code := rt.tradePurchaseAdmission(division, c, npc); code != 0 {
						refusal = code
						return false
					}
				}
			}
			capacity := uint16(inventory.BagEnd(c) - inventory.EquipmentSlotEnd)
			if container != nil {
				capacity = uint16(container.Capacity)
			}
			dest, grantErr := commerce.GrantPackage(inv, contents, q.Quantity, capacity)
			if grantErr != nil {
				refusal = commerceFailureCode(grantErr)
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
			// Port-only, not native (halloween.go): a sealed event
			// helmet is never sold, however sellable its row is.
			if rt.HalloweenHelmetSeal.Sealed(item.Codename) {
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
			if code := rt.tradeAdmission(division, c, npc, item); code != 0 {
				refusal = code
				return false
			}
			// The native special-trade tail: above the stack cap 0x1815, above the held count
			// 0x1814, both after the merchant's admission.
			if q.Quantity > rt.maxStackFor(item.TypeFlags, item.Codename) {
				refusal = tradeErrOverStack
				return false
			}
			if q.Quantity > item.Quantity {
				refusal = wire.ErrCodeInputFewerThanRemain
				return false
			}
			if q.Quantity == 0 {
				return false
			}
			price, restore, priced := commerce.SalePrices(ref, item.MagicOptions, rt.Commerce.Magic, rt.commerceTax(division, npc, c))
			if !priced || price > math.MaxInt64/uint64(q.Quantity) || restore > math.MaxInt64/uint64(q.Quantity) {
				return false
			}
			credit := price * uint64(q.Quantity)
			if inventory.IsTradeGoods(item.TypeFlags) {
				part := item
				part.Quantity = q.Quantity
				var profit int64
				// One snapshot of the live tunables prices the sale and splits it.
				settings := tunables.Current()
				credit, profit, collected, priced = rt.tradeSaleValue(division, c, npc, part, settings)
				if !priced {
					return false
				}
				payouts = rt.tradeRewards(division, c, roster, commerce.TradeRewardInput{Credit: int64(credit), Profit: profit},
					settings)
				for _, payout := range payouts {
					if payout.Gold < 0 || uint64(payout.Gold) > math.MaxInt64-goldOf(payout.character) {
						return false
					}
				}
				if profit > 0 {
					weeklyCredit = credit
					// The reference server routes the same ten-percent contribution to the
					// shard's hunter pool for traders or thief pool for thieves.
					if !settledPool.Credit(c.Job.Type, int64(float64(credit)*0.1)) {
						return false
					}
				}
				credit = uint64(payouts[0].Gold)
			}
			if credit > math.MaxInt64-balance {
				return false
			}
			if _, fault := inv.DropQuantity(q.SourceSlot, q.Quantity); fault != nil {
				refusal = commerceFailureCode(fault)
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
		frames = append(frames, wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: response}, wire.Frame{Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: balance}.Encode()})
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
		var recipients []RecipientFrames
		for _, payout := range payouts {
			if payout.character.ID != c.ID {
				setGold(payout.character, goldOf(payout.character)+uint64(payout.Gold))
			}
			experience, _ := rt.addJobExperience(payout.character, payout.Experience)
			if payout.character.ID == c.ID {
				frames = append(frames, experience...)
			} else {
				private := append([]wire.Frame{goldFrame(payout.character)}, experience...)
				recipients = append(recipients, RecipientFrames{CharacterID: payout.character.ID, Frames: private})
			}
		}
		if weeklyCredit > 0 && c.Job.Type == domain.JobTrader {
			c.Job.WeeklyReward = commerce.AddWeeklyTradeReward(c.Job.WeeklyReward, int32(float64(weeklyCredit)*0.1))
		}
		*pool = settledPool
		result = OpResult{Frames: frames, Broadcast: broadcasts, Recipients: recipients}
		return true
	})
	if !committed {
		return failureResult(refusal)
	}
	if honorCost > 0 {
		rt.spendHonorPoints(division, snapshot, uint32(honorCost))
	}
	if guildPointCost > 0 {
		result = rt.spendGuildPoints(division, snapshot, uint32(guildPointCost), result)
	}
	rt.collectFortressTax(division, npc, collected)
	return result
}

/*
================
offerUnitPrice

Gold prices carry the town tax; guild-point and honor prices are the
authored points.
================
*/
func offerUnitPrice(o commerce.Offer, tax commerce.Tax) (uint64, bool) {
	if o.Currency == commerce.PaymentHonor || o.Currency == commerce.PaymentGuildPoint {
		return o.Price, o.Price > 0
	}
	return commerce.AdjustPrice(o.Price, tax, true)
}

/*
================
honorPoints

Returns the character's usable Training Camp honor points via the runtime's
HonorPoints hook (natively the Training Camp member record's
[member+0x34]+0x54), or 0 when unconfigured.
================
*/
func (rt *Runtime) honorPoints(division string, c *enterworld.Character) uint64 {
	if rt == nil || rt.HonorPoints == nil || c == nil {
		return 0
	}
	return uint64(rt.HonorPoints(division, c))
}

/*
================
spendHonorPoints

Deducts cost honor points via the runtime's SpendHonorPoints hook after a
shop purchase commits.
================
*/
func (rt *Runtime) spendHonorPoints(division string, c *enterworld.Character, cost uint32) {
	if rt == nil || rt.SpendHonorPoints == nil || c == nil || cost == 0 {
		return
	}
	_ = rt.SpendHonorPoints(division, c, cost)
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
		bodyValue := item.Body()
		bodyValue.Summon.RefreshRentalTimes(rt.Now().Unix())
		body := bodyValue.Encode()
		if len(body) == 0 {
			return nil, fmt.Errorf("invalid shop item body in slot %d", item.Slot)
		}
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
	return failureResult(commerceFailureCode(err))
}

/*
================
commerceFailureCode

The native refusal code commerceFailure answers with.
================
*/
func commerceFailureCode(err error) uint8 {
	var fault *inventory.Fault
	if errors.As(err, &fault) {
		return fault.Code
	}
	var insufficient domain.MallInsufficientCurrency
	if errors.As(err, &insufficient) {
		return wire.ErrCodeMallInsufficientCurrency
	}
	return wire.ErrCodeInvalidRequest
}
