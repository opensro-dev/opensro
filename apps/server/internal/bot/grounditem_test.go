package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
)

func ownedDropRow(gid, owner uint32, withTail bool) []byte {
	row := wire.GroundItemRow{
		RefObjID: 12345,
		Gid:      gid,
		Position: wire.Position{RegionID: 23197, X: 812, Y: 75, Z: 392, Heading: 0x1000},
		HasOwner: 1,
		OwnerJID: owner,
		Tint:     0,
		WithAppearTail: withTail,
		AppearFlag:     1,
	}
	return row.Encode()
}

func TestGroundItemFromRowMatchesOwnDrop(t *testing.T) {
	const selfGID uint32 = 404417
	item, ok := bot.GroundItemFromRow(ownedDropRow(9001, selfGID, false), selfGID)
	if !ok {
		t.Fatal("an owned drop was not parsed")
	}
	if item.GID != 9001 || item.Region != 23197 || item.OwnerJID != selfGID {
		t.Fatalf("item = %+v", item)
	}
	if item.X != 812 || item.Z != 392 {
		t.Fatalf("position = %v,%v", item.X, item.Z)
	}
}

func TestGroundItemFromRowHandlesAppearTail(t *testing.T) {
	const selfGID uint32 = 404417
	if _, ok := bot.GroundItemFromRow(ownedDropRow(9002, selfGID, true), selfGID); !ok {
		t.Fatal("a fresh drop with the appear tail was not parsed")
	}
}

func TestGroundItemFromRowRejectsForeignDrop(t *testing.T) {
	if _, ok := bot.GroundItemFromRow(ownedDropRow(9003, 999, false), 404417); ok {
		t.Fatal("a foreign drop was accepted")
	}
}

func TestGroundItemFromRowRejectsUnknownOwner(t *testing.T) {
	if _, ok := bot.GroundItemFromRow(ownedDropRow(9004, 404417, false), 0); ok {
		t.Fatal("a drop was accepted before the bot learned its object id")
	}
}
