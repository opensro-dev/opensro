/*
===========================================================================

grounditem.go - parse the ground-item rows the server drops

A monster kill leaves one or more CIItem ground entities. The server sends
each as a single-object 0x30D7 spawn (a fresh drop, with the appear tail) or
inside an object-list 0x3417 chunk (a drop already on the ground, without the
tail). The row layout depends on the item type-flag word, which the bot does
not carry. The bot recovers the row by trying each band shape and keeping the
one whose owner field names the bot's own object id: every drop the bot earned
carries its killer as OwnerJID for OwnerLifetime.

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
)

// OpcodeSingleObjectSpawn is the 0x30D7 single-object spawn. A fresh drop
// rides this form with the appear tail; a drop already on the ground rides an
// object-list chunk instead.
const OpcodeSingleObjectSpawn uint16 = wire.OpSingleObjectSpawn

// groundItemBands is the band byte count of the wire.GroundItemRow layouts
// (grounditemrow.go): the ETC band carries nothing, the equipment band a
// discard byte, the codename band an empty u16 label, the gold band the heap
// amount. The bot tries each and keeps the owner match.
var groundItemBands = [...]int{0, 1, 2, 4}

// groundItemGoldBand is the gold heap band byte count (the u32 amount).
const groundItemGoldBand = 4

// GroundItem is one ground entity the bot owns.
type GroundItem struct {
	RefObjID uint32
	GID      uint32
	Region   uint16
	X        float64
	Y        float64
	Z        float64
	// HasOwner is the wire owner flag. OwnerJID is the temporary pickup
	// reservation: the killer's object id while the drop is reserved.
	HasOwner bool
	OwnerJID uint32
	// IsGold reports a gold heap (the gold band).
	IsGold bool
}

// GroundItemFromRow returns the ground item when the row is a CIItem spawn
// owned by selfGID. The band and tail shape is recovered by the owner match:
// a misread band shifts the fields and cannot land the exact owner id.
func GroundItemFromRow(payload []byte, selfGID uint32) (GroundItem, bool) {
	if selfGID == 0 {
		return GroundItem{}, false
	}
	for _, band := range groundItemBands {
		for _, tail := range []bool{false, true} {
			item, ok := parseGroundItemRow(payload, band, tail)
			if !ok || !item.HasOwner || item.OwnerJID != selfGID {
				continue
			}
			return item, true
		}
	}
	return GroundItem{}, false
}

// parseGroundItemRow decodes one CIItem spawn row of the given band width and
// optional appear tail. It reports false unless the row consumes exactly and
// the fields are plausible, so a wrong shape is rejected before the owner
// check.
func parseGroundItemRow(payload []byte, band int, tail bool) (GroundItem, bool) {
	r := &rowReader{data: payload}
	item := GroundItem{RefObjID: r.u32()}
	r.bytes(band) // band-specific field, not needed here
	item.GID = r.u32()
	item.Region = r.u16()
	item.X = r.f32()
	item.Y = r.f32()
	item.Z = r.f32()
	r.u16() // heading
	owner := r.u8()
	if owner != 0 {
		item.OwnerJID = r.u32()
	}
	r.u8() // tint
	if tail {
		r.u8() // appear flag
	}
	if r.err != nil || r.pos != len(payload) {
		return GroundItem{}, false
	}
	if owner > 1 || item.GID == 0 || item.Region == 0 {
		return GroundItem{}, false
	}
	if !plausibleCoordinate(item.X) || !plausibleCoordinate(item.Y) || !plausibleCoordinate(item.Z) {
		return GroundItem{}, false
	}
	item.HasOwner = owner != 0
	item.IsGold = band == groundItemGoldBand
	return item, true
}

// plausibleCoordinate rejects the denormal and huge floats a misread band
// produces. Region-local coordinates are small; the bound is generous.
func plausibleCoordinate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= -1e6 && v <= 1e6
}
