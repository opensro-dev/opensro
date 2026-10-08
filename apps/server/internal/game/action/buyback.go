package action

import (
	"encoding/binary"
	"encoding/json"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"strconv"
)

// Explicit browser extension, not a guessed retail buyback opcode.
// Request: version:u8=1, selected merchant:u32, immutable entry ID:u32.
// Reply: versioned JSON; id=0 publishes the merchant's updated ledger.
const opShopBuyback uint16 = 13

// v1.150 68f480 evicts the oldest at five; 688c20 removes by ordinal.
const buybackLimit = 5

type buybackOffer struct {
	Preview  shopInventoryRow `json:"preview"`
	Index    uint8            `json:"index"`
	ID       uint32           `json:"id"`
	Ref      uint32           `json:"refObjId"`
	Name     string           `json:"name"`
	Quantity uint16           `json:"quantity"`
	Price    string           `json:"price"`
	Plus     int64            `json:"plus"`
}

func (rt *Runtime) buybackOffers(c *enterworld.Character, merchant uint32) []buybackOffer {
	rows := []buybackOffer{}
	if c == nil {
		return rows
	}
	for index, e := range retailBuybackEntries(c) {
		name := e.Item.Codename
		if ref, ok := rt.deps.ItemReferences().ItemRefByCodename(name); ok && ref != nil {
			name = ref.Name
		}
		previews, err := rt.shopInventoryRows(invItemsFromRowsWithin([]enterworld.InventoryRow{e.Item}, int64(inventory.MaxBagEnd)), nil)
		if err != nil || len(previews) != 1 {
			continue
		}
		rows = append(rows, buybackOffer{previews[0], uint8(index), e.ID, e.Item.RefObjID, name, uint16(e.Item.StackCount), strconv.FormatUint(e.Price, 10), e.Item.Plus})
	}
	return rows
}
func (rt *Runtime) buybackFrame(c *enterworld.Character, npc, merchant, id uint32, items []shopInventoryRow, fault string) wire.Frame {
	if items == nil {
		items = []shopInventoryRow{}
	}
	b, _ := json.Marshal(struct {
		Version int                `json:"version"`
		Npc     uint32             `json:"npc"`
		ID      uint32             `json:"id"`
		Entries []buybackOffer     `json:"entries"`
		Items   []shopInventoryRow `json:"items"`
		Error   string             `json:"error,omitempty"`
	}{1, npc, id, rt.buybackOffers(c, merchant), items, fault})
	return wire.Frame{Opcode: opShopBuyback, Payload: b}
}

// Called under the character authority door; all edits remain in local values
// until inventory encoding and the complete transaction have succeeded.
func retainSale(c *enterworld.Character, merchant uint32, item inventory.Item, quantity uint16, price uint64) ([]domain.BuybackEntry, uint32, bool) {
	if c.BuybackNext == ^uint32(0) {
		return nil, 0, false
	}
	item.Quantity = quantity
	row := rowsFromInvItems([]inventory.Item{item})[0]
	row.MagicOptions = append([]uint64(nil), row.MagicOptions...)
	id := c.BuybackNext + 1
	entries := append([]domain.BuybackEntry(nil), c.Buyback...)
	if len(entries) >= buybackLimit {
		entries = entries[len(entries)-buybackLimit+1:]
	}
	entries = append(entries, domain.BuybackEntry{ID: id, MerchantRef: merchant, Price: price, Item: row})
	return entries, id, true
}
func (rt *Runtime) HandleBuyback(division string, c *enterworld.Character, p []byte) OpResult {
	if c == nil || len(p) != 9 || p[0] != 1 {
		return OpResult{}
	}
	npcID, id := binary.LittleEndian.Uint32(p[1:]), binary.LittleEndian.Uint32(p[5:])
	if npcID == 0 || id == 0 {
		return OpResult{}
	}
	return rt.handleBuyback(division, c, npcID, id, nil)
}

func retailBuybackEntries(c *enterworld.Character) []domain.BuybackEntry {
	entries := c.Buyback
	if len(entries) > buybackLimit {
		entries = entries[len(entries)-buybackLimit:]
	}
	return entries
}

// Native request 693b00: selected NPC u32, ReStoreIndex u8.
func (rt *Runtime) HandleRetailBuyback(division string, c *enterworld.Character, p []byte) OpResult {
	if c == nil || len(p) != 5 {
		return OpResult{}
	}
	npc := binary.LittleEndian.Uint32(p)
	if npc == 0 {
		return OpResult{}
	}
	index := p[4]
	return rt.handleBuyback(division, c, npc, 0, &index)
}

func (rt *Runtime) handleBuyback(division string, c *enterworld.Character, npcID, id uint32, ordinal *uint8) OpResult {
	refuse := func(snapshot *enterworld.Character, merchant uint32, message string) OpResult {
		if ordinal != nil {
			return OpResult{Frames: []wire.Frame{{Opcode: 0xb7e7, Payload: []byte{2, wire.ErrCodeInvalidRequest}}}}
		}
		return OpResult{Frames: []wire.Frame{rt.buybackFrame(snapshot, npcID, merchant, id, nil, message)}}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, c)
	npc, admitted := rt.commerceNpc(division, snapshot, npcID)
	if !admitted {
		return refuse(snapshot, 0, "Select a nearby merchant")
	}
	var result OpResult
	committed := rt.deps.Update(c, "shop-buyback", func() bool {
		if c.DeletePending {
			return false
		}
		index := -1
		visible := retailBuybackEntries(c)
		if ordinal != nil {
			entries := visible
			if int(*ordinal) >= len(entries) {
				return false
			}
			id = entries[*ordinal].ID
		}
		for i, e := range visible {
			if e.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return false
		}
		e := visible[index]
		balance := goldOf(c)
		if e.Price > balance {
			return false
		}
		before := invItemsFromBag(c)
		inv := inventory.New(before, inventory.BagEnd(c))
		item := invItemsFromRowsWithin([]enterworld.InventoryRow{e.Item}, int64(inventory.MaxBagEnd))[0]
		// Retain the sold stack as one complete object; no metadata-losing merge.
		if _, err := inv.Grant(item); err != nil {
			return false
		}
		rows, err := rt.shopInventoryRows(inv.Items(), before)
		if err != nil {
			return false
		}
		entries := append([]domain.BuybackEntry(nil), visible[:index]...)
		entries = append(entries, visible[index+1:]...)
		if len(rows) != 1 {
			return false
		}
		reference := rt.commerceReferences(inv.Items(), before)
		c.MissionInventory = rowsFromInvItems(inv.Items())
		c.Buyback = entries
		setGold(c, balance-e.Price)
		result = OpResult{Broadcast: []wire.Frame{reference}, Frames: []wire.Frame{reference, rt.buybackFrame(c, npcID, npc.RefObjID, id, rows, ""), {Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: balance - e.Price}.Encode()}}}
		if ordinal != nil {
			restore := wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: wire.NewWriter(6).U8(1).U8(0x22).U8(rows[0].Slot).U8(*ordinal).U16(uint16(e.Item.StackCount)).Payload()}
			result.Frames = append(result.Frames[:2], append([]wire.Frame{restore}, result.Frames[2:]...)...)
			result.Frames = append(result.Frames, wire.Frame{Opcode: 0xb7e7, Payload: []byte{1}})
		}
		return true
	})
	if !committed {
		return refuse(rt.characterSnapshot(division, c), npc.RefObjID, "Buyback unavailable: check the item, gold and bag space")
	}
	return result
}
