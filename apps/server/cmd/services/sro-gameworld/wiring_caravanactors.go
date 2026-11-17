/*
===========================================================================

wiring_caravanactors.go - port-only caravan actors on the existing player lane

Actors are transient authority characters: normal equipped appearance,
normal PvP validation and skill damage, and normal transport cargo drops.
They have no login account and never enter the persisted character roster.

===========================================================================
*/
package main

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

const caravanActorIDBase int64 = 10_000_000
const caravanRoadNodesPerTick = 64

/*
================
caravanRunSpeed
================
*/
func (game *gameplayPlane) caravanRunSpeed(c *enterworld.Character) float32 {
	if c.ActiveCOS != nil {
		if refs, ok := game.deps.Items.(enterworld.CharacterRefSource); ok {
			if ref, found := refs.CharacterRefByCodename(c.ActiveCOS.Codename); found {
				return ref.RunSpeed
			}
		}
	}
	return simulation.RunSpeed
}

/*
================
caravanAttackSkill
================
*/
func (game *gameplayPlane) caravanAttackSkill(c *enterworld.Character) uint32 {
	code := "SKILL_CH_SWORD_BASE_01"
	weapon := c.MissionInventory[0].Codename
	if strings.Contains(weapon, "_SPEAR_") || strings.Contains(weapon, "_TBLADE_") {
		code = "SKILL_CH_SPEAR_BASE_01"
	}
	if strings.Contains(weapon, "_BOW_") {
		code = "SKILL_CH_BOW_BASE_01"
	}
	if source, ok := game.deps.Skills.(enterworld.SkillCodenameSource); ok {
		if skill, found := source.SkillByCodename(code); found {
			return skill.ID
		}
	}
	return 0
}

/*
================
initCaravanActors
================
*/
func (game *gameplayPlane) initCaravanActors() error {
	registry, ok := game.deps.Characters.(interface {
		RegisterRuntimeCharacters(string, []*domain.Character) error
	})
	if !ok {
		return fmt.Errorf("character authority cannot register caravan actors")
	}
	seed := enterworld.DefaultSkillSeeder(game.deps.Skills)
	for i, unit := range game.caravanAgents.EnsureDivisionRoster(game.divisionID) {
		gender := int64(0)
		modelCode := "CHAR_CH_MAN_MERCHANT"
		if strings.Contains(unit.JobSuitCode, "_F_") {
			gender, modelCode = 1, "CHAR_CH_WOMAN_ADVENTURER"
		}
		model := game.deps.Roster.ModelByCodename(modelCode)
		if model == nil {
			return fmt.Errorf("caravan model %s unavailable", modelCode)
		}
		modelRef, level, strength, intellect := int64(model.RefObjID), int64(70), int64(225), int64(155)
		c := &enterworld.Character{RuntimeOnly: true, ID: caravanActorIDBase + int64(i), Name: unit.Name,
			ModelCodename: modelCode, ModelRef: &modelRef, Gender: &gender, Level: &level, MaxLevel: &level, Strength: &strength, Intellect: &intellect,
			Job: domain.CharacterJob{Type: caravanJob(unit.Role), Grade: 3, Alias: unit.Name}}
		var err error
		c.Skills, err = seed(enterworld.RaceKeyChina, nil)
		if err != nil {
			return err
		}
		for _, equip := range []struct {
			slot int64
			code string
		}{{6, unit.WeaponCode}, {int64(enterworld.JobSuitSlot), unit.JobSuitCode}} {
			ref, found := game.deps.Items.ItemRefByCodename(equip.code)
			if !found {
				return fmt.Errorf("caravan equipment %s unavailable", equip.code)
			}
			c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: equip.slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), VarianceBits: "0", Durability: 5000, StackCount: 1})
		}
		if strings.Contains(unit.WeaponCode, "_BOW_") {
			if ref, found := game.deps.Items.ItemRefByCodename("ITEM_ETC_AMMO_ARROW_01"); found {
				c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: 7, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 10000})
			}
		}
		region, x, y, z := int64(unit.RegionID), float64(unit.X), float64(unit.Y), float64(unit.Z)
		c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z}, SpawnSet: true}
		if unit.Role == caravanai.RoleTrader {
			c.RuntimeTransportGID = uint32(20_000_000 + i)
			c.ActiveCOS, err = game.items.NewServerActorTransport(c.RuntimeTransportGID)
			if err != nil {
				return err
			}
		}
		game.caravanActors = append(game.caravanActors, c)
	}
	if err := registry.RegisterRuntimeCharacters(game.divisionID, game.caravanActors); err != nil {
		return err
	}
	for _, c := range game.caravanActors {
		_, hp, _, mp := game.items.GameplayVitals(game.divisionID, c)
		game.caravanAgents.ConfigureVitals(game.divisionID, c.Name, hp, mp, game.caravanAttackSkill(c), game.caravanRunSpeed(c))
		game.deps.Mutate(c, "caravan actor init", func() { c.CurrentHP, c.CurrentMP = &hp, &mp })
		game.items.BindPetSession(game.divisionID, c, uint64(c.ID))
	}
	game.items.ServerActorHit = func(division, victim, attacker string, damage int, nowMs int64) {
		game.caravanAgents.RecordPlayerAttack(division, victim, attacker, damage, nowMs)
	}
	return nil
}

/*
================
caravanJob
================
*/
func caravanJob(role caravanai.AgentRole) uint8 {
	switch role {
	case caravanai.RoleTrader:
		return domain.JobTrader
	case caravanai.RoleHunter:
		return domain.JobHunter
	default:
		return domain.JobThief
	}
}

/*
================
caravanMove

Every step uses the normal ground clip. Blocked legs ask the existing
outdoor detour planner; missing geometry never authorizes a straight walk.
================
*/
func (game *gameplayPlane) caravanMove(name string, from, to caravanai.Waypoint, distance float32) (caravanai.Waypoint, bool) {
	if caravanai.WaypointDistance(from, to) < 0.01 {
		return from, true
	}
	if game.caravanRoads == nil {
		game.caravanRoads = make(map[string]*caravanRoad)
	}
	road := game.caravanRoads[name]
	if road != nil && (road.goal.RegionID != to.RegionID || road.goal.X != to.X || road.goal.Z != to.Z) {
		delete(game.caravanRoads, name)
		road = nil
	}
	plane := movement.NewPlanes(game.movement, game.water).For(from.RegionID)
	a := simulation.Spawn{RegionID: from.RegionID, X: float64(from.X), Y: float64(from.Y), Z: float64(from.Z)}
	target := to
	if road != nil {
		if road.route == nil {
			road.route = road.search.Advance(caravanRoadNodesPerTick)
		}
		if road.route == nil || road.route.Status() != monster.NavigationRouteReady {
			return from, false
		}
		if road.index < road.route.Len() {
			p := road.route.Point(road.index)
			target = caravanai.Waypoint{RegionID: p.RegionID, X: float32(p.X), Y: float32(p.Y), Z: float32(p.Z)}
		}
	}
	region, x, z, arrived := caravanai.StepWaypoint(from.RegionID, from.X, from.Z, target, distance)
	goal := simulation.Spawn{RegionID: region, X: float64(x), Y: a.Y, Z: float64(z)}
	if road != nil {
		goal.Y = float64(target.Y)
	}
	path := plane.PlanPath(monster.Pose{RegionID: a.RegionID, X: a.X, Y: a.Y, Z: a.Z}, simulation.NavOwner{}, monster.Pose{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z})
	if path == nil || path.Result()&(monster.NavResultBlocked|monster.NavResultClipped) != 0 {
		if road == nil {
			leg := to
			game.caravanRoads[name] = &caravanRoad{goal: to, search: game.water.NewRoadSearch(monster.Pose{RegionID: a.RegionID, X: a.X, Y: a.Y, Z: a.Z}, monster.Pose{RegionID: leg.RegionID, X: float64(leg.X), Y: float64(leg.Y), Z: float64(leg.Z)})}
		}
		return from, false
	}
	next := path.Rest()
	if arrived && road != nil {
		road.index++
		if road.index < road.route.Len() {
			arrived = false
		} else {
			delete(game.caravanRoads, name)
			arrived = caravanai.WaypointDistance(caravanai.Waypoint{RegionID: next.RegionID, X: float32(next.X), Z: float32(next.Z)}, to) < 0.01
		}
	}
	if arrived {
		delete(game.caravanRoads, name)
	}
	return caravanai.Waypoint{RegionID: next.RegionID, X: float32(next.X), Y: float32(next.Y), Z: float32(next.Z)}, arrived
}

/*
================
caravanRoad
================
*/
type caravanRoad struct {
	goal   caravanai.Waypoint
	search *movement.RoadSearch
	route  *monster.NavigationRoute
	index  int
}

/*
================
caravanPlayer
================
*/
func (game *gameplayPlane) caravanPlayer(division, name string) (caravanai.PlayerPosition, bool) {
	c := game.deps.CharacterByName(division, name)
	if c == nil || c.RuntimeOnly {
		return caravanai.PlayerPosition{}, false
	}
	s, online := game.presence.SessionByName(division, name)
	if !online || !s.WorldReady() {
		return caravanai.PlayerPosition{}, false
	}
	var snapshot *enterworld.Character
	game.deps.Read(division, func() { snapshot = c.Snapshot() })
	if domain.CharacterWorldInstance(snapshot) != domain.DefaultWorldInstance {
		return caravanai.PlayerPosition{}, false
	}
	at := game.items.LiveSpawnFor(division, snapshot)
	return caravanai.PlayerPosition{ObservedPlayer: caravanai.ObservedPlayer{Name: name, JobType: enterworld.DressedJob(snapshot), HPPercent: game.playerHPPercent(snapshot)}, At: caravanai.Waypoint{RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z)}}, true
}

/*
================
publishCaravanActor
================
*/
func (game *gameplayPlane) publishCaravanActor(ticker *simulation.Ticker, views []simulation.SessionView, previous, unit *caravanai.AgentUnit, c *enterworld.Character, nowMs int64) {
	at := simulation.Spawn{RegionID: unit.RegionID, X: float64(unit.X), Y: float64(unit.Y), Z: float64(unit.Z)}
	c = game.items.SyncServerActor(game.divisionID, c, at, unit.RespawnPending, unit.CargoStacks, unit.OriginTown == caravanai.TownDonwhang)
	if unit.RespawnPending {
		game.caravanAgents.ActorSynchronized(game.divisionID, c.Name)
	}
	appearance := simulation.PeerAppearance{RefObjID: uint32(*c.ModelRef), Name: c.Name, JobType: c.Job.Type, JobGrade: c.Job.Grade, Run: unit.RunSpeed}
	for slot := int64(0); slot <= int64(enterworld.JobSuitSlot); slot++ {
		for _, row := range c.MissionInventory {
			if row.Slot == slot {
				appearance.Equipment = append(appearance.Equipment, wire.PlayerEquipItem{RefObjID: row.RefObjID, TypeFlags: row.TypeFlags})
			}
		}
	}
	gid := enterworld.ObjectIDForCharacter(c)
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		shown := false
		for _, id := range view.PublishedObjects {
			if id == gid {
				shown = true
				break
			}
		}
		viewer := view.World.LiveSpawnAt(nowMs)
		visible := view.WorldInstance == domain.DefaultWorldInstance && worldgeom.InterestVisible(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z}, worldgeom.RegionXZ{RegionID: viewer.RegionID, X: viewer.X, Z: viewer.Z})
		var frames []simulation.Frame
		if visible && !shown {
			frames = append(frames, game.items.PeerItemReferences(appearance.ItemIDs())...)
			frames = append(frames, simulation.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: simulation.BuildPeerSpawnRow(appearance, gid, at), ScopeGID: gid, ScopeVisible: true})
		} else if !visible && shown {
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode(), ScopeGID: gid})
		}
		if visible && shown && (previous.RegionID != unit.RegionID || previous.X != unit.X || previous.Z != unit.Z) {
			from := simulation.MovementSourceFromSpawn(simulation.Spawn{RegionID: previous.RegionID, X: float64(previous.X), Y: float64(previous.Y), Z: float64(previous.Z)})
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: simulation.RunMode}).Encode()}, simulation.Frame{Opcode: simulation.OpMovementAck, Payload: simulation.BuildMovementAckPayload(gid, simulation.MovementRequest{Mode: simulation.MovementAckDestinationMode, RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, &from)})
		}
		if c.ActiveCOS != nil {
			pet := c.ActiveCOS
			petShown := false
			for _, id := range view.PublishedObjects {
				if id == pet.GID {
					petShown = true
					break
				}
			}
			if visible && !petShown {
				row := wire.CosSpawnBand2{Band: 2, RefObjID: pet.RefObjID, Gid: pet.GID, Position: wire.Position{RegionID: at.RegionID, X: unit.X - 10, Y: unit.Y, Z: unit.Z}, Walk: 20, Run: 50, Scale: 100, Name: pet.Name, OwnerName: c.Name, OwnerGid: gid}
				frames = append(frames, simulation.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(row), ScopeGID: pet.GID, ScopeVisible: true})
			} else if !visible && petShown {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: pet.GID}).Encode(), ScopeGID: pet.GID})
			} else if visible && petShown {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: (wire.ObjectSourceCorrection{Gid: pet.GID, Position: wire.Position{RegionID: at.RegionID, X: unit.X - 10, Y: unit.Y, Z: unit.Z}}).Encode()})
			}
		}
		if len(frames) > 0 {
			ticker.Push.PushToSession(view.SessionID, frames)
		}
	}
}

/*
================
publishCaravanAction
================
*/
func (game *gameplayPlane) publishCaravanAction(c *enterworld.Character, result action.OpResult) {
	action.BroadcastObservedFrames(game.hub, game.divisionID, 0, enterworld.ObjectIDForCharacter(c), result.Broadcast)
	for _, around := range result.Around {
		action.BroadcastObservedFrames(game.hub, game.divisionID, 0, around.SourceGID, around.Frames)
	}
	for _, recipient := range result.Recipients {
		for _, session := range game.hub.SessionsInDivision(game.divisionID) {
			peer, _, ok := enterworld.SessionCharacter(game.deps, session)
			if ok && peer.ID == recipient.CharacterID && session.WorldReady() {
				for _, frame := range recipient.Frames {
					_ = session.Send(frame.Opcode, frame.Payload)
				}
			}
		}
	}
}
