/*
===========================================================================

skillobjects.go - action adapters for native quest-skill world objects

The registry owns scan lifetime, transport owns published scope and quests
own captured-item rewards. This adapter binds those owners under the same
division serialization used by item admission and monster combat.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
================
SpawnQuestMonster

The quest minute and quest item use already hold division and character
authority. Resolve only the live population and pose here; do not open a
nested character read door. The quest owner names the monster, its ring
and its timer; a request without a FixedPosition lands around the character.
================
*/
func (rt *Runtime) SpawnQuestMonster(division string, c *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
	if rt.Monsters == nil || c == nil || !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 {
		return false
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		return false
	}
	request.Division, request.Population, request.NowMs = division, lease, rt.Now().UnixMilli()
	if !request.FixedPosition {
		request.Position = rt.liveSpawn(simulation.WorldKey(division, c.Name), c, request.NowMs)
	}
	return rt.Monsters.SpawnQuestMonster(request)
}

/*
================
useQuestTrap

49C2B0 -> 59B8D0 creates the indirect skill at its owner. The item debit
follows successful world-object admission inside the existing character door.
================
*/
func (rt *Runtime) useQuestTrap(c *enterworld.Character, use skillItemUse, skill enterworld.SkillRow, result *OpResult) bool {
	lease, present := rt.EntryPopulationLease(use.division, c.Name)
	if !present || rt.CaptureQuestTrap == nil || rt.CanPlaceQuestTrap == nil || c.NativeTeleportMode != 0 {
		result.DiagnosticRefusal = "quest-trap: owner or quest service unavailable"
		return false
	}
	frames, admitted := rt.CanPlaceQuestTrap(c, skill.Codename)
	if !admitted {
		result.Frames = append(result.Frames, frames...)
		result.DiagnosticRefusal = "quest-trap: quest admission refused"
		return false
	}
	trap := skill.CastGate.QuestTrap
	if code := rt.qestRefusal(use.division, c, trap.Radius, use.nowMs); code != 0 {
		result.DiagnosticRefusal = "quest-trap: placement prerequisite refused"
		return false
	}
	at := rt.liveSpawn(simulation.WorldKey(use.division, c.Name), c, use.nowMs)
	_, err := rt.SkillObjects.Create(skillobject.Object{
		Division: use.division, Population: lease,
		OwnerGID: enterworld.ObjectIDForCharacter(c), OwnerName: c.Name, CreatedMs: use.nowMs,
		Program: skillobject.Program{SkillID: skill.ID, DurationMs: trap.DurationMs,
			ScanMs: trap.ScanMs, Radius: trap.Radius, Targets: trap.Targets},
		Spawn: wire.SkillObjectSpawn{Region: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle},
	})
	if err != nil {
		result.DiagnosticRefusal = err.Error()
		return false
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	result.Frames = []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

/*
================
SkillObjectRows

Bootstrap uses exactly the same lease and visibility predicate as live scope.
================
*/
func (rt *Runtime) SkillObjectRows(division string, c *enterworld.Character, entry *enterworld.LocalPlayerEntry) []enterworld.Packet {
	if c == nil || entry == nil {
		return nil
	}
	lease, present := rt.EntryPopulationLease(division, c.Name)
	if !present {
		return nil
	}
	viewer := skillobject.Viewer{Division: division, Population: lease, CharacterGID: enterworld.ObjectIDForCharacter(c), Position: worldgeom.RegionXZ{
		RegionID: uint16(entry.StartProfile.RegionID), X: entry.StartProfile.X, Z: entry.StartProfile.Z,
	}}
	var rows []enterworld.Packet
	for _, object := range rt.SkillObjects.Snapshot() {
		if !skillobject.Visible(object, viewer) {
			continue
		}
		row := enterworld.NewPacket(enterworld.OpcodeObjectListChunk, object.Spawn.Encode(false))
		row.Scope = []domain.ObjectScopeChange{{GID: object.Spawn.GID, Visible: true}}
		rows = append(rows, row)
	}
	return rows
}

/*
================
AdvanceSkillObjects

Tick after ordinary action updates so death and travel retire the owner
before a trap can dispatch a new capture. Each viewer receives spawn, result
and retirement in one ordered batch, including first-tick detonations.
================
*/
func (rt *Runtime) AdvanceSkillObjects(nowMs int64, sessions []simulation.SessionSnapshot) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	before := rt.SkillObjects.Snapshot()
	var results []simulation.DivisionFrames
	for _, object := range before {
		for _, batch := range rt.advanceSkillObject(object, nowMs) {
			if batch.SourceGID == object.Spawn.GID {
				results = append(results, batch)
			} else {
				out = append(out, batch)
			}
		}
	}
	after := rt.SkillObjects.Snapshot()
	for _, session := range sessions {
		if session.PublishedObjects == nil {
			continue
		}
		pose := session.World.LiveSpawnAt(nowMs)
		viewer := skillobject.Viewer{
			Division: session.DivisionID, Population: session.Population, Published: session.PublishedObjects,
			CharacterGID: simulation.PlayerObjectID(session.CharacterID),
			Position:     worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z},
		}
		frames := simFrames(skillobject.ScopeFrames(before, viewer))
		// This is a prediction inside one reliable batch, not another scope
		// cache. Transport commits all Scope changes only after admission.
		published := make([]uint32, 0, len(before))
		for _, object := range before {
			if !skillobject.Visible(object, viewer) {
				continue
			}
			published = append(published, object.Spawn.GID)
			for _, batch := range results {
				if batch.DivisionID == object.Division && batch.SourceGID == object.Spawn.GID {
					frames = append(frames, batch.Frames...)
				}
			}
		}
		viewer.Published = published
		frames = append(frames, simFrames(skillobject.ScopeFrames(after, viewer))...)
		if len(frames) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: session.DivisionID,
				OnlyCharacterID: session.CharacterID, Frames: frames})
		}
	}
	return out
}

/*
================
advanceSkillObject

48D690 checks the monster AI's first opponent (+BC), not its summoner or
loot owner. A matching trap retires before the quest handler rolls capture.
================
*/
func (rt *Runtime) advanceSkillObject(object skillobject.Object, nowMs int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(object.Division)
	defer unlock()
	c := rt.findCharacter(object.Division, object.OwnerName)
	snapshot := rt.characterSnapshot(object.Division, c)
	lease, present := rt.EntryPopulationLease(object.Division, object.OwnerName)
	ownerPresent := present && lease == object.Population && snapshot != nil && !snapshot.DeletePending &&
		enterworld.CharacterAlive(snapshot) && snapshot.NativeTeleportMode == 0 &&
		enterworld.ObjectIDForCharacter(snapshot) == object.OwnerGID &&
		domain.CharacterWorldInstance(snapshot) == uint32(object.Population.ID)
	if object.Program.Field {
		rt.advanceSkillField(object, snapshot, ownerPresent, nowMs)
		return nil
	}
	if object.Program.Pulse {
		return rt.advanceTrapField(object, c, snapshot, ownerPresent, lease, nowMs)
	}
	var targets []skillobject.Target
	if ownerPresent && nowMs >= object.NextScanMs && rt.Monsters != nil {
		from := simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X),
			Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
		for _, candidate := range rt.Monsters.CombatCandidatesInPopulation(object.Division, lease, from,
			float64(object.Program.Radius), nowMs, true) {
			mover, exists := rt.Monsters.Mover(object.Division, candidate.Gid)
			if !exists {
				continue
			}
			pose := mover.LivePoseAt(nowMs, nil)
			targets = append(targets, skillobject.Target{GID: candidate.Gid, RefID: candidate.Ref.RefObjID,
				OwnerGID: candidate.Opponents[0].GID, Alive: candidate.CurrentHP > 0,
				Region: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
		}
	}
	if object.Program.Combat && ownerPresent {
		ownerPresent = combatTrapOwnerNear(object, rt.liveSpawn(simulation.WorldKey(object.Division, object.OwnerName), snapshot, nowMs))
	}
	_, targetGID, retired := rt.SkillObjects.Scan(object.Spawn.GID, nowMs, ownerPresent, targets)
	if retired && object.Program.Combat {
		rt.retireCombatTrapEffect(object.Division, c, object, nowMs)
	}
	if retired && targetGID != 0 && object.Program.Combat {
		target, exists := rt.Monsters.GetInPopulation(object.Division, lease, targetGID)
		skill, known := rt.deps.SkillData().SkillByID(object.Program.SkillID)
		if !exists || target.CurrentHP == 0 || !known || !skill.CombatTrap.Pinned {
			return nil
		}
		// Only a monster triggers (48D690 reads its AI's first opponent); the
		// explosion strikes players too (combatTrapVictims).
		primary := combatTarget{gid: target.Gid, monster: &target, at: rt.monsterSpawn(object.Division, target.Gid, nowMs)}
		return rt.explodeCombatTrap(object, c, snapshot, skill, primary, lease, nowMs)
	}
	if !retired || targetGID == 0 || rt.CaptureQuestTrap == nil {
		return nil
	}
	target, exists := rt.Monsters.GetInPopulation(object.Division, lease, targetGID)
	if !exists || target.CurrentHP == 0 {
		return nil
	}
	var frames []wire.Frame
	skills, available := rt.deps.SkillData().(interface {
		SkillByID(uint32) (enterworld.SkillRow, bool)
	})
	if !available {
		return nil
	}
	skill, available := skills.SkillByID(object.Program.SkillID)
	if !available {
		return nil
	}
	rt.deps.Update(c, "quest-trap-capture", func() bool {
		if !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 {
			return false
		}
		var captured bool
		frames, captured = rt.CaptureQuestTrap(c, skill.Codename, target.Ref.Codename, func() bool {
			return rt.Monsters.Defeat(object.Division, targetGID, time.UnixMilli(nowMs))
		})
		return captured
	})
	if len(frames) == 0 {
		return nil
	}
	return []simulation.DivisionFrames{{DivisionID: object.Division, OnlyCharacterID: c.ID, Frames: simFrames(frames)}}
}
