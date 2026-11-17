/*
===========================================================================
wiring_caravancombat.go - port-only caravan combat through native action owners
===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"slices"
)

/*
================
caravanCast
================
*/
func (game *gameplayPlane) caravanCast(c *enterworld.Character, target uint32, nowMs int64) action.OpResult {
	if game.caravanAttackAt == nil {
		game.caravanAttackAt = map[string]int64{}
	}
	if nowMs < game.caravanAttackAt[c.Name] {
		return action.OpResult{}
	}
	game.caravanAttackAt[c.Name] = nowMs + 500
	skills := game.caravanSkills[c.Name]
	skill := game.caravanAttackSkill(c)
	if len(skills) > 0 {
		var cooldowns []enterworld.EntrySkillCooldown
		game.deps.Read(game.divisionID, func() { cooldowns = enterworld.RemainingSkillCooldowns(game.deps.Skills, c, nowMs) })
		for offset := range len(skills) {
			candidate := skills[(int(nowMs/500)+offset)%len(skills)]
			if !slices.ContainsFunc(cooldowns, func(row enterworld.EntrySkillCooldown) bool { return row.Skill == candidate }) {
				skill = candidate
				break
			}
		}
	}
	result := game.items.HandleTargetInteract(game.divisionID, c, (wire.SkillAction{ActionId: skill, HasTarget: true, TargetGid: target}).Encode())
	return result
}

/*
================
caravanNPCThreat
================
*/
func (game *gameplayPlane) caravanNPCThreat(unit *caravanai.AgentUnit, nowMs int64) (uint32, caravanai.Waypoint) {
	if game.items.Monsters == nil || unit.Role == caravanai.RoleTrader {
		return 0, caravanai.Waypoint{}
	}
	lease, ok := game.items.EntryPopulationLease(game.divisionID, unit.Name)
	if !ok {
		return 0, caravanai.Waypoint{}
	}
	from := caravanai.Waypoint{RegionID: unit.RegionID, X: unit.X, Y: unit.Y, Z: unit.Z}
	var gid uint32
	var at caravanai.Waypoint
	distance := float64(160)
	for _, npc := range game.items.Monsters.PopulationInterestInstances(game.divisionID, lease, worldgeom.RegionXZ{RegionID: unit.RegionID, X: float64(unit.X), Z: float64(unit.Z)}, nowMs) {
		if npc.CurrentHP == 0 || !(npc.ThiefMonster() || npc.HunterMonster()) || npc.JobAttackerRefused(caravanJob(unit.Role)) {
			continue
		}
		mover, exists := game.items.Monsters.Mover(game.divisionID, npc.Gid)
		if !exists {
			continue
		}
		pose := mover.LivePoseAt(nowMs, nil)
		target := caravanai.Waypoint{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z)}
		if d := caravanai.WaypointDistance(from, target); d < distance {
			distance, gid, at = d, npc.Gid, target
		}
	}
	return gid, at
}

/*
================
caravanCombatants

Detached AI target/census inputs. They have no socket or visibility recipient.
================
*/
func (game *gameplayPlane) caravanCombatants() []simulation.SessionSnapshot {
	var out []simulation.SessionSnapshot
	for _, c := range game.caravanActors {
		lease, ok := game.items.EntryPopulationLease(game.divisionID, c.Name)
		if !ok {
			continue
		}
		snap := simulation.SessionSnapshot{DivisionID: game.divisionID, CharacterID: c.ID, WorldInstance: 0, Population: lease, BodyRadius: 6, Appearance: &simulation.PeerAppearance{JobType: enterworld.DressedJob(c)}}
		game.deps.Read(game.divisionID, func() { snap.CombatEligible = enterworld.CharacterAlive(c) })
		snap.COS = game.items.PetPresentation(game.divisionID, c.Name)
		snap.World = game.items.Worlds.Snapshot(simulation.WorldKey(game.divisionID, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
		out = append(out, snap)
	}
	return out
}
