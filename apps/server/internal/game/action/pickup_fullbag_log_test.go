/*
===========================================================================

pickup_fullbag_log_test.go - the full-bag pickup refusal's log level (#2441)

===========================================================================
*/
package action

import (
	"testing"

	log "github.com/sirupsen/logrus"
	testhook "github.com/sirupsen/logrus/hooks/test"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPickupFullBagRefusalLogsInfoNotWarn

#2441: a full bag refusing a ground pickup is the player's own
outcome, so the B-11 bag-state line logs at info; warnings are
reserved for faults. The refusal itself is unchanged.
================
*/
func TestPickupFullBagRefusalLogsInfoNotWarn(t *testing.T) {
	hook := testhook.NewGlobal()
	defer hook.Reset()

	character := testCharacter()
	character.MissionInventory = nil
	for slot := int64(13); slot < 45; slot++ {
		character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
		})
	}
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	added := rt.Ground.Add(testDivision, PlanItemDrop(inventory.Item{
		RefObjID: 999, Codename: "ITEM_CH_RING_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 5, 3), Quantity: 1,
	}, 1, start, "someone", clock.Now()))

	result := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: added.Gid}.Encode())
	if got := result.Frames[0].Payload; got[0] != 0x02 || got[1] != wire.ErrCodeStorageFull {
		t.Fatalf("refusal = % X, want [02 07] inventory-full", got)
	}

	lines := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message != "pickup: ground item refused, bag full" {
			continue
		}
		if entry.Level != log.InfoLevel {
			t.Fatalf("refusal logged at %s, want info", entry.Level)
		}
		if entry.Data["character"] != character.Name ||
			entry.Data["occupied"] == nil || entry.Data["capacity"] == nil {
			t.Fatalf("refusal lost its B-11 bag-state fields: %v", entry.Data)
		}
		lines++
	}
	if lines != 1 {
		t.Fatalf("refusal lines = %d, want 1", lines)
	}
}
