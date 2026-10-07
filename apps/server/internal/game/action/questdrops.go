package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"time"
)

func (rt *Runtime) planQuestKillDrops(c *enterworld.Character, target monster.Instance, pose monster.Pose, now int64) []grounditem.Item {
	if rt.QuestMonsterDrops == nil || rt.deps.ItemReferences() == nil {
		return nil
	}
	var drops []grounditem.Item
	for _, amount := range rt.QuestMonsterDrops(c, target.Ref.Codename, target.Rarity(), rt.DropRoll) {
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(amount.Codename)
		if !ok || ref == nil || amount.Count == 0 || amount.Count > 65535 {
			continue
		}
		drop := PlanItemDrop(inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: uint16(amount.Count)}, uint16(amount.Count), simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z, Angle: pose.Heading}, c.Name, time.UnixMilli(now))
		drop.OwnerJID = enterworld.ObjectIDForCharacter(c)
		drop = rt.scatterMonsterDrop(drop, simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z, Angle: pose.Heading}, target.Rarity())
		drops = append(drops, drop)
	}
	return drops
}
