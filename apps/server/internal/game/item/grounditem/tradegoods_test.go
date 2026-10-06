/*
===========================================================================

tradegoods_test.go - dropped cargo keeps its original owner through restart

===========================================================================
*/
package grounditem

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCargoOwnerSurvivesGroundPersistence
================
*/
func TestCargoOwnerSurvivesGroundPersistence(t *testing.T) {
	registry := NewRegistry()
	item := registry.Add("division", Item{RefObjID: 2151, TypeFlags: 0x46c, StackCount: 17, TradeOwner: "OriginalAlias"})
	encoded, err := json.Marshal(registry.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var persisted domain.GroundSnapshot
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	restored := NewRegistry()
	restored.Restore(persisted)
	rows := restored.All("division")
	if len(rows) != 1 || rows[0].Gid != item.Gid || rows[0].TradeOwner != "OriginalAlias" || rows[0].StackCount != 17 {
		t.Fatalf("cargo changed after reload: %+v", rows)
	}
}
