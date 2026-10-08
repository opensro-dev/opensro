package action

import (
	"encoding/json"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

func TestStorageIdentityReconnectAndGroundSnapshot(t *testing.T) {
	const id = uint64(0xfedcba9876543210)
	inv := inventory.New([]inventory.Item{{Slot: 13, RefObjID: 3630, Quantity: 20, RecordID: id, MagicOptions: []uint64{123}}}, domain.DefaultInventorySize)
	rows := rowsFromInvItems(inv.Items())
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	var restored []domain.InventoryRow
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	inv = inventory.New(invItemsFromRowsWithin(restored, int64(domain.DefaultInventorySize)), domain.DefaultInventorySize)
	dropped, fault := inv.DropQuantity(13, 20)
	if fault != nil || dropped.RecordID != id {
		t.Fatalf("full drop lost identity: %+v %v", dropped, fault)
	}
	ground := PlanItemDrop(dropped, 20, simulation.Spawn{}, "test", time.Now())
	registry := grounditem.NewRegistry()
	registry.Add("test", ground)
	bytes, err := json.Marshal(registry.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domain.GroundSnapshot
	if err := json.Unmarshal(bytes, &snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := grounditem.NewRegistry()
	restarted.Restore(snapshot)
	persisted := restarted.Snapshot().Divisions["test"]
	if len(persisted) != 1 || persisted[0].RecordID != id {
		t.Fatalf("ground reconnect lost identity: %+v", persisted)
	}
	if persisted[0].Gid == uint32(id&0xffffffff) {
		t.Fatal("record identity was confused with runtime ID")
	}
}
