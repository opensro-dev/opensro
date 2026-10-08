/*
===========================================================================

stall_network.go - the stall network: listing, searching and buying

A stall opened with the network chosen while its owner stands in a town is
listed by its offers' network categories: textdata/fmntidgroupmapdata.txt
maps an item's TypeID tuple to a category (the client's 7E1330 reads the
same table and refuses an item with none, 0x42), fmncategorytreedata.txt
names the categories. A player in a town searches a category fifteen rows
a page (CIFStallNetwork, 766FC0: rows * 0xF) and buys a row from afar
(7671D0); the row must still be what the search showed (0x4F), and the
owner pays the network's 1% commission.

The search's last byte is the small-category combo's index (id 43,
CIFStallNetwork_SendSearch 5ABB90): the degree, 0 for any. INFERENCE: an
item's degree is (ItemClass-1)/3+1, as alchemy reads it.

INFERENCE: outside a town a search answers 0x49, which closes the window
(766FC0), and an untown purchase 0x49 too; the server keeps no search
throttle beyond the client's ten seconds (UIIT_MSG_WARENETWORK_SCAN_TOOLTIP).

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
)

const (
	// stallNetworkPageRows is CIFStallNetwork's rows per page (766FC0).
	stallNetworkPageRows  = 15
	stallNetworkGroupFile = "fmntidgroupmapdata.txt"
)

/*
================
ConfigureStallNetwork

Loads the TypeID -> network category table. A missing or malformed table
fails construction: the network cannot list items it cannot classify.
================
*/
func (rt *Runtime) ConfigureStallNetwork(textdataDir string) error {
	rows := enterworld.ReadTextdataFile(filepath.Join(textdataDir, stallNetworkGroupFile))
	if len(rows) == 0 {
		return fmt.Errorf("stall network: %s is empty or missing", stallNetworkGroupFile)
	}
	groups := make(map[[4]int64]uint32, len(rows))
	for i, row := range rows {
		if len(row) < 6 {
			return fmt.Errorf("stall network: %s row %d has %d columns", stallNetworkGroupFile, i+1, len(row))
		}
		if row[0] != "1" {
			continue
		}
		var tid [4]int64
		group, err := strconv.ParseUint(row[1], 10, 32)
		if err != nil {
			return fmt.Errorf("stall network: %s row %d group: %w", stallNetworkGroupFile, i+1, err)
		}
		for j := range tid {
			if tid[j], err = strconv.ParseInt(row[2+j], 10, 64); err != nil {
				return fmt.Errorf("stall network: %s row %d type: %w", stallNetworkGroupFile, i+1, err)
			}
		}
		groups[tid] = uint32(group)
	}
	rt.StallCategories = groups
	return nil
}

/*
================
stallCategory

The network category of an item (0: not listable).
================
*/
func (rt *Runtime) stallCategory(item inventory.Item) uint32 {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return 0
	}
	return rt.StallCategories[ref.TypeIDs]
}

/*
================
stallDegree

An item's degree (0 when it has none).
================
*/
func (rt *Runtime) stallDegree(item inventory.Item) uint8 {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return 0
	}
	class := int(ref.NativeFields.Get("itemClass"))
	if class <= 0 {
		return 0
	}
	return uint8((class-1)/3 + 1)
}

/*
================
inTown

A region that forbids player combat is a town (_RefRegion.IsBattleField).
================
*/
func (rt *Runtime) inTown(division string, c *enterworld.Character) bool {
	allowed, known := world.RegionPlayerCombat(rt.LiveSpawnFor(division, c).RegionID)
	return known && !allowed
}

/*
================
characterRiding
================
*/
func (rt *Runtime) characterRiding(_ string, c *enterworld.Character) bool {
	return c.ActiveCOS != nil && c.ActiveCOS.Mounted
}

/*
================
HandleStallNetworkSearch

0x76F9 [u8 kind][u8 page][u32 category][u8 degree].
================
*/
func (rt *Runtime) HandleStallNetworkSearch(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	_, _ = r.U8()
	page, _ := r.U8()
	category, _ := r.U32()
	degree, _ := r.U8()
	if r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.inTown(division, c) {
		return stallAnswer(wire.OpStallNetworkResult, wire.StallErrNetworkTown)
	}
	var found []wire.StallListing
	for _, listing := range rt.Stalls.Network(division, category) {
		owner := rt.findCharacter(division, listing.Owner)
		if owner == nil {
			continue
		}
		snapshot := rt.characterSnapshot(division, owner)
		item, ok := bagOf(snapshot).At(listing.Slot.BagSlot)
		if !ok || degree != 0 && rt.stallDegree(item) != degree {
			continue
		}
		body := item.Body()
		body.Quantity = listing.Slot.Quantity
		found = append(found, wire.StallListing{
			Item:     body,
			Owner:    enterworld.ObjectIDForCharacter(owner),
			Slot:     listing.Index,
			Quantity: listing.Slot.Quantity,
			Price:    uint64(listing.Slot.Price),
			Serial:   listing.Slot.Serial,
		})
	}
	pages := (len(found) + stallNetworkPageRows - 1) / stallNetworkPageRows
	start := min(int(page)*stallNetworkPageRows, len(found))
	rows := found[start:min(start+stallNetworkPageRows, len(found))]
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpStallNetworkResult, Payload: wire.EncodeStallNetworkResult(rows, uint8(min(pages, 0xFF)))}}}
}

/*
================
HandleStallNetworkBuy

0x72CA [u32 owner][u8 slot][u64 price][u16 count][u8][u64 serial].
================
*/
func (rt *Runtime) HandleStallNetworkBuy(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	ownerGid, _ := r.U32()
	slot, _ := r.U8()
	price, _ := r.U64()
	count, _ := r.U16()
	_, _ = r.U8()
	serial, _ := r.U64()
	if r.Done() != nil || c == nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refuse := func(code uint8) OpResult { return stallAnswer(wire.OpStallNetworkBought, code) }
	if !rt.inTown(division, c) {
		return refuse(wire.StallErrNetworkTown)
	}
	owner := rt.findCharacterByGid(division, ownerGid)
	if owner == nil || owner.ID == c.ID {
		return refuse(wire.StallErrNetworkStale)
	}
	s, ok := rt.Stalls.Get(division, owner.Name)
	if !ok || !s.Network || int(slot) >= len(s.Slots) || s.Slots[slot] == nil ||
		uint64(s.Slots[slot].Price) != price || s.Slots[slot].Quantity != count {
		return refuse(wire.StallErrNetworkStale)
	}
	if code := rt.sellStallSlot(division, owner.Name, slot, c, wire.StallEventNetworkSold, serial); code != 0 {
		return refuse(code)
	}
	return stallAnswer(wire.OpStallNetworkBought, 0)
}
