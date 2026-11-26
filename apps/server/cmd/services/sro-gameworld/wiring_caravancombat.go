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
	if game.items.ServerActorCasting(game.divisionID, c.Name) {
		return action.OpResult{}
	}
	if game.caravanAttackAt == nil {
		game.caravanAttackAt = map[string]int64{}
	}
	if nowMs < game.caravanAttackAt[c.Name] {
		return action.OpResult{}
	}
	game.caravanAttackAt[c.Name] = nowMs + 500
	skills := game.caravanSkills[c.Name]
	basicSkill := game.caravanAttackSkill(c)
	skill := basicSkill
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

	// Native B245 insufficient-MP refusal 0x3004 carries only its low error byte.
	if slices.ContainsFunc(result.Frames, func(frame wire.Frame) bool {
		return frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) == 2 && frame.Payload[0] == 2 && frame.Payload[1] == 4
	}) {
		if game.caravanAgents != nil {
			game.caravanAgents.RequestManaRecovery(game.divisionID, c.Name)
		}
		if skill != basicSkill && basicSkill != 0 {
			result = game.items.HandleTargetInteract(game.divisionID, c, (wire.SkillAction{ActionId: basicSkill, HasTarget: true, TargetGid: target}).Encode())
		}
	}
	return result
}

/*
================
caravanCastSkill

Casts a specific trained skill (such as Force Resurrection) at a target GID
through the native HandleTargetInteract path.
================
*/
func (game *gameplayPlane) caravanCastSkill(c *enterworld.Character, skillID, target uint32, nowMs int64) action.OpResult {
	if skillID == 0 || game.items.ServerActorCasting(game.divisionID, c.Name) {
		return action.OpResult{}
	}
	if game.caravanAttackAt == nil {
		game.caravanAttackAt = map[string]int64{}
	}
	if nowMs < game.caravanAttackAt[c.Name] {
		return action.OpResult{}
	}
	game.caravanAttackAt[c.Name] = nowMs + 1000
	return game.items.HandleTargetInteract(game.divisionID, c, (wire.SkillAction{ActionId: skillID, HasTarget: true, TargetGid: target}).Encode())
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
	var leaderGID, leaderCOSGID uint32
	leaderFrom := from
	hasLeader := false
	leaderName := ""
	if unit.Role == caravanai.RoleHunter && unit.EscortingTrader != "" {
		leaderName = unit.EscortingTrader
	} else if unit.HiredBy != "" {
		leaderName = unit.HiredBy
	}
	if leaderName != "" {
		if leader := game.deps.CharacterByName(game.divisionID, leaderName); leader != nil {
			hasLeader = true
			leaderGID = enterworld.ObjectIDForCharacter(leader)
			game.deps.Read(game.divisionID, func() {
				if leader.ActiveCOS != nil {
					leaderCOSGID = leader.ActiveCOS.GID
				}
			})
			at := game.items.LiveSpawnFor(game.divisionID, leader)
			leaderFrom = caravanai.Waypoint{RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z)}
		}
	}

	var bestGID uint32
	var bestAt caravanai.Waypoint
	bestDist := float64(260)
	var priorityGID uint32
	var priorityAt caravanai.Waypoint
	priorityDist := float64(300)

	scan := func(center caravanai.Waypoint) {
		for _, npc := range game.items.Monsters.PopulationInterestInstances(game.divisionID, lease, worldgeom.RegionXZ{RegionID: center.RegionID, X: float64(center.X), Z: float64(center.Z)}, nowMs) {
			if npc.CurrentHP == 0 || !(npc.ThiefMonster() || npc.HunterMonster()) || npc.JobAttackerRefused(caravanJob(unit.Role)) {
				continue
			}
			if game.isCaravanThreatIgnored(unit.Name, npc.Gid, nowMs) {
				continue
			}
			mover, exists := game.items.Monsters.Mover(game.divisionID, npc.Gid)
			if !exists {
				continue
			}
			pose := mover.LivePoseAt(nowMs, nil)
			target := caravanai.Waypoint{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z)}
			targetGID := mover.TargetGID()
			if hasLeader && (targetGID == leaderGID || (leaderCOSGID != 0 && targetGID == leaderCOSGID)) {
				if d := caravanai.WaypointDistance(leaderFrom, target); d < priorityDist {
					priorityDist, priorityGID, priorityAt = d, npc.Gid, target
				}
			}
			if d := caravanai.WaypointDistance(from, target); d < bestDist {
				bestDist, bestGID, bestAt = d, npc.Gid, target
			}
		}
	}

	scan(from)
	if hasLeader && caravanai.WaypointDistance(from, leaderFrom) > 20 {
		scan(leaderFrom)
	}
	if priorityGID != 0 {
		return priorityGID, priorityAt
	}
	return bestGID, bestAt
}

func (game *gameplayPlane) ignoreCaravanThreat(name string, gid uint32, untilMs int64) {
	if game.caravanIgnoredThreats == nil {
		game.caravanIgnoredThreats = make(map[string]map[uint32]int64)
	}
	byGID := game.caravanIgnoredThreats[name]
	if byGID == nil {
		byGID = make(map[uint32]int64)
		game.caravanIgnoredThreats[name] = byGID
	}
	byGID[gid] = untilMs
}

func (game *gameplayPlane) isCaravanThreatIgnored(name string, gid uint32, nowMs int64) bool {
	if game.caravanIgnoredThreats == nil {
		return false
	}
	byGID := game.caravanIgnoredThreats[name]
	if byGID == nil {
		return false
	}
	until, ok := byGID[gid]
	if !ok {
		return false
	}
	if nowMs >= until {
		delete(byGID, gid)
		return false
	}
	return true
}

/*
================
caravanObject

Whether gid is a caravan actor or its transport (Ticker.RuntimeObject).
The set is written once at boot, before the ticker starts, and only read
after.
================
*/
func (game *gameplayPlane) caravanObject(gid uint32) bool {
	return game.caravanObjects[gid]
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
