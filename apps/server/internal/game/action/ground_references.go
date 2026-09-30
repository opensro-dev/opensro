package action

import (
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
)

var seededDropReferences = func() map[string]bool {
	known := map[string]bool{}
	for _, code := range loot.MonsterDropRefItemCodenames() {
		known[code] = true
	}
	return known
}()

// Reference deltas precede every newly introduced ground type for both the
// actor and observers. Do not put all possible loot into world bootstrap.
// Re-entry separately seeds actual Ground contents through GroundRefItemCodenames.
func (rt *Runtime) groundReferences(drops []grounditem.Item) []wire.Frame {
	rows := []inventory.Item{}
	seen := map[uint32]bool{}
	for _, drop := range drops {
		if drop.IsGold() || seededDropReferences[drop.Codename] || seen[drop.RefObjID] {
			continue
		}
		seen[drop.RefObjID] = true
		rows = append(rows, inventory.Item{RefObjID: drop.RefObjID, Codename: drop.Codename, TypeFlags: drop.TypeFlags})
	}
	var frames []wire.Frame
	for len(rows) > 0 {
		n := min(len(rows), 64)
		frames = append(frames, rt.commerceReferences(rows[:n], nil))
		rows = rows[n:]
	}
	return frames
}
