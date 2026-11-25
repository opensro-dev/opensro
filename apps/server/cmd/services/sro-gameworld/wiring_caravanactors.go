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
	"math"
	"slices"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

type caravanMovePublish struct {
	atMs      int64
	dirX      float64
	dirZ      float64
	mode      uint8
	goal      worldgeom.RegionXZ
	stopTicks int
}

const caravanActorIDBase int64 = 90_000

// caravanHunterRunSpeed is an AI Hunter's run speed (port-only).
const caravanHunterRunSpeed float32 = 75
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
	// Hunters run at 75 (port-only, they must catch caravans); everyone
	// else at the native speed, which the 50% speed potion raises to the
	// same 75 (keepCaravanSpeed).
	if c.Job.Type == domain.JobHunter {
		return caravanHunterRunSpeed
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
	cap, err := progression.LevelCapFromEnv()
	if err != nil {
		return err
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
		modelRef, level, strength, intellect := int64(model.RefObjID), cap, int64(20+4*(cap-1)), int64(20+cap-1)
		c := &enterworld.Character{RuntimeOnly: true, ID: caravanActorIDBase + int64(i), Name: unit.Name,
			ModelCodename: modelCode, ModelRef: &modelRef, Gender: &gender, Level: &level, MaxLevel: &level, Strength: &strength, Intellect: &intellect,
			Job: domain.CharacterJob{Type: caravanJob(unit.Role), Grade: 3, Alias: unit.Name}}
		var err error
		c.Skills, err = seed(enterworld.RaceKeyChina, nil)
		if err != nil {
			return err
		}
		if err := game.equipCaravanActor(c, unit, i%2 == 1); err != nil {
			return err
		}
		if err := game.trainCaravanActor(c); err != nil {
			return err
		}

		if strings.Contains(unit.WeaponCode, "_BOW_") {
			if ref, found := game.deps.Items.ItemRefByCodename("ITEM_ETC_AMMO_ARROW_01"); found {
				c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: 7, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 10000})
			}
		}
		region, x, y, z := int64(unit.RegionID), float64(unit.X), float64(unit.Y), float64(unit.Z)
		c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z}, SpawnSet: true}
		if unit.Role == caravanai.RoleTrader || unit.Role == caravanai.RoleThief {
			var valid bool
			c.RuntimeTransportGID, valid = enterworld.CosObjectIDForCharacter(c)
			if !valid {
				return fmt.Errorf("caravan owner cannot own a transport: %s", c.Name)
			}
			if unit.Role == caravanai.RoleTrader {
				c.ActiveCOS, err = game.items.NewServerActorTransport(c.RuntimeTransportGID)
				if err != nil {
					return err
				}
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
		game.caravanAgents.SetCanResurrect(game.divisionID, c.Name, game.caravanRessSkill[c.Name] != 0)
		game.deps.Mutate(c, "caravan actor init", func() { c.CurrentHP, c.CurrentMP = &hp, &mp })
		game.items.BindPetSession(game.divisionID, c, uint64(c.ID))
		if game.items.Monsters != nil {
			if err := game.items.AdmitServerActor(game.divisionID, c.Name); err != nil {
				return err
			}
		}
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
	if road != nil && caravanai.WaypointDistance(road.goal, to) > 64 {
		delete(game.caravanRoads, name)
		road = nil
	}
	if road != nil && road.failed {
		if road.cooldown > 1 && caravanai.WaypointDistance(road.from, from) < 1 && caravanai.WaypointDistance(road.goal, to) < 16 {
			road.cooldown--
			return from, false
		}
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
		if road.route == nil {
			return from, false
		}
		if road.route.Status() != monster.NavigationRouteReady {
			road.failed = true
			road.cooldown = 10
			road.from = from
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
			game.caravanRoads[name] = &caravanRoad{from: from, goal: to, search: game.water.NewRoadSearch(monster.Pose{RegionID: a.RegionID, X: a.X, Y: a.Y, Z: a.Z}, monster.Pose{RegionID: leg.RegionID, X: float64(leg.X), Y: float64(leg.Y), Z: float64(leg.Z)})}
			return from, false
		}
		if path != nil && path.Result()&monster.NavResultBlocked == 0 {
			rest := path.Rest()
			nextWp := caravanai.Waypoint{RegionID: rest.RegionID, X: float32(rest.X), Y: float32(rest.Y), Z: float32(rest.Z)}
			if caravanai.WaypointDistance(from, nextWp) >= 0.25 {
				if caravanai.WaypointDistance(nextWp, target) < 1.0 && road.index+1 < road.route.Len() {
					road.index++
				} else {
					delete(game.caravanRoads, name)
				}
				return nextWp, false
			}
		}
		delete(game.caravanRoads, name)
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
	from     caravanai.Waypoint
	goal     caravanai.Waypoint
	search   *movement.RoadSearch
	route    *monster.NavigationRoute
	index    int
	failed   bool
	cooldown int
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
	c = game.items.SyncServerActorCargo(game.divisionID, c, at, unit.RespawnPending, unit.CargoStacks, unit.CargoStars, unit.OriginTown == caravanai.TownDonwhang)
	if unit.RespawnPending {
		game.caravanAgents.ActorSynchronized(game.divisionID, c.Name)
	}
	appearance := simulation.PeerAppearance{RefObjID: uint32(*c.ModelRef), Name: c.Name, JobType: c.Job.Type, JobGrade: c.Job.Grade, Run: unit.RunSpeed}
	// An agent waiting for hire wears no job suit and carries a guild-row
	// label ("[Hunter for hire]"), so players can tell who is free.
	forHire := unit.AvailableForHire && !unit.RespawnPending
	if forHire {
		appearance.JobType, appearance.JobGrade = 0, 0
		appearance.GuildName = unit.Role.Title() + " for hire"
	}
	for slot := int64(0); slot <= int64(enterworld.JobSuitSlot); slot++ {
		if forHire && slot == int64(enterworld.JobSuitSlot) {
			continue
		}
		for _, row := range c.MissionInventory {
			if row.Slot == slot {
				appearance.Equipment = append(appearance.Equipment, wire.PlayerEquipItem{RefObjID: row.RefObjID, TypeFlags: row.TypeFlags})
			}
		}
	}
	if game.caravanLook == nil {
		game.caravanLook = make(map[string]bool)
	}
	lookChanged := game.caravanLook[c.Name] != forHire
	game.caravanLook[c.Name] = forHire
	moveMode := simulation.RunMode
	if unit.Walking {
		moveMode = simulation.WalkMode
	}
	gid := enterworld.ObjectIDForCharacter(c)
	moveGID := gid
	if c.ActiveCOS != nil && c.ActiveCOS.Mounted {
		moveGID = c.ActiveCOS.GID
	}
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
		if (unit.RespawnPending || lookChanged) && shown {
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode(), ScopeGID: gid})
			shown = false
		}
		if game.caravanMoving == nil {
			game.caravanMoving = make(map[string]bool)
			game.caravanMode = make(map[string]uint8)
			game.caravanMovePub = make(map[string]caravanMovePublish)
		}
		moveKey := view.SessionID + "|" + c.Name
		if visible && !shown {
			delete(game.caravanMoving, moveKey)
			delete(game.caravanMovePub, moveKey)
			frames = append(frames, game.items.PeerItemReferences(appearance.ItemIDs())...)
			frames = append(frames, simulation.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: simulation.BuildPeerSpawnRow(appearance, gid, at), ScopeGID: gid, ScopeVisible: true})
			if !enterworld.CharacterAlive(c) {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelLife, Value: wire.LifeStateDead}).Encode()})
			}
		} else if !visible && shown {
			delete(game.caravanMoving, moveKey)
			delete(game.caravanMovePub, moveKey)
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode(), ScopeGID: gid})
		}
		moved := previous.RegionID != unit.RegionID || previous.X != unit.X || previous.Z != unit.Z
		canMove := visible && shown && (moveGID == gid || slices.Contains(view.PublishedObjects, moveGID))
		wasMoving := game.caravanMoving[moveKey]
		snapped := false
		if canMove && moved {
			prevXZ := worldgeom.RegionXZ{RegionID: previous.RegionID, X: float64(previous.X), Z: float64(previous.Z)}
			curXZ := worldgeom.RegionXZ{RegionID: unit.RegionID, X: float64(unit.X), Z: float64(unit.Z)}
			dx, dz := worldgeom.Delta(prevXZ, curXZ)
			stepLen := math.Hypot(dx, dz)
			if stepLen > 32 {
				frames = append(frames,
					simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: gid}).Encode(), ScopeGID: gid},
					simulation.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: simulation.BuildPeerSpawnRow(appearance, gid, at), ScopeGID: gid, ScopeVisible: true},
				)
				game.caravanMoving[moveKey] = false
				delete(game.caravanMovePub, moveKey)
				snapped = true
			} else if stepLen > 0.01 {
				dirX, dirZ := dx/stepLen, dz/stepLen
				pub := game.caravanMovePub[moveKey]
				pub.stopTicks = 0
				dot := dirX*pub.dirX + dirZ*pub.dirZ
				remX, remZ := worldgeom.Delta(curXZ, pub.goal)
				needSend := !wasMoving || pub.mode != moveMode || dot < 0.96 || nowMs-pub.atMs >= 350 || math.Hypot(remX, remZ) <= 12
				if !wasMoving || game.caravanMode[moveKey] != moveMode {
					frames = append(frames, simulation.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: moveGID, StateType: wire.StateChannelMove, Value: moveMode}).Encode()})
					game.caravanMode[moveKey] = moveMode
				}
				if needSend {
					// Project ~500ms ahead from the actor's live position (not previous) so
					// the client glides smoothly without 10Hz rubber-banding.
					proj := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: curXZ.RegionID, X: curXZ.X + 5*dx, Z: curXZ.Z + 5*dz})
					goal := simulation.Spawn{RegionID: proj.RegionID, X: proj.X, Y: float64(unit.Y), Z: proj.Z}
					from := simulation.MovementSourceFromSpawn(at)
					frames = append(frames, simulation.Frame{Opcode: simulation.OpMovementAck, Payload: simulation.BuildMovementAckPayload(moveGID, simulation.MovementRequest{Mode: simulation.MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &from)})
					pub = caravanMovePublish{atMs: nowMs, dirX: dirX, dirZ: dirZ, mode: moveMode, goal: proj}
				}
				game.caravanMovePub[moveKey] = pub
				game.caravanMoving[moveKey] = true
			}
		} else if canMove && wasMoving {
			pub := game.caravanMovePub[moveKey]
			pub.stopTicks++
			if pub.stopTicks >= 2 {
				from := simulation.MovementSourceFromSpawn(at)
				frames = append(frames,
					simulation.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: moveGID, StateType: wire.StateChannelMove, Value: wire.MoveStateStand}).Encode()},
					simulation.Frame{Opcode: simulation.OpMovementAck, Payload: simulation.BuildMovementAckPayload(moveGID, simulation.MovementRequest{Mode: simulation.MovementAckDestinationMode, RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, &from)},
				)
				game.caravanMoving[moveKey] = false
				delete(game.caravanMovePub, moveKey)
			} else {
				game.caravanMovePub[moveKey] = pub
			}
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
			if (unit.RespawnPending || snapped) && petShown {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: pet.GID}).Encode(), ScopeGID: pet.GID})
				petShown = false
			}
			if visible && !petShown {
				row := wire.CosSpawnBand2{Band: 2, RefObjID: pet.RefObjID, Gid: pet.GID, Position: wire.Position{RegionID: at.RegionID, X: unit.X, Y: unit.Y, Z: unit.Z}, Walk: 20, Run: unit.RunSpeed, Scale: 100, Name: pet.Name, OwnerName: c.Name, OwnerGid: gid}
				frames = append(frames, simulation.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: wire.EncodeCosSpawnBand2(row), ScopeGID: pet.GID, ScopeVisible: true})
			} else if !visible && petShown {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: pet.GID}).Encode(), ScopeGID: pet.GID})
			} else if visible && petShown && !pet.Mounted {
				frames = append(frames, simulation.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: (wire.ObjectSourceCorrection{Gid: pet.GID, Position: wire.Position{RegionID: at.RegionID, X: unit.X, Y: unit.Y, Z: unit.Z}}).Encode()})
			}
			if visible && pet.Mounted && (!petShown || !shown || snapped) {
				frames = append(frames, simulation.Frame{Opcode: wire.OpCosRideState, Payload: wire.EncodeCosRideState(gid, true, pet.GID)})
			}
		} else if c.RuntimeTransportGID != 0 && slices.Contains(view.PublishedObjects, c.RuntimeTransportGID) {
			frames = append(frames, simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: (wire.ObjectDespawn{Gid: c.RuntimeTransportGID}).Encode(), ScopeGID: c.RuntimeTransportGID})
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
	gid := enterworld.ObjectIDForCharacter(c)
	cast := slices.ContainsFunc(result.Broadcast, func(frame wire.Frame) bool {
		return frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) > 2 && frame.Payload[0] == 1
	})
	if cast {
		stopped := false
		for key := range game.caravanMoving {
			if strings.HasSuffix(key, "|"+c.Name) && game.caravanMoving[key] {
				delete(game.caravanMoving, key)
				delete(game.caravanMovePub, key)
				stopped = true
			}
		}
		if stopped {
			// The caravan's projected peer path lives outside the native movement segment.
			at := game.items.LiveSpawnFor(game.divisionID, c)
			stop := wire.ObjectSourceCorrection{Gid: gid, Position: wire.Position{
				RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle,
			}}
			action.BroadcastObservedFrames(game.hub, game.divisionID, 0, gid, []wire.Frame{{Opcode: wire.OpObjectSourceCorrection, Payload: stop.Encode()}})
		}
	}
	action.BroadcastObservedFrames(game.hub, game.divisionID, 0, gid, result.Broadcast)
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
