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
SpawnQuestGuardian

The quest minute already holds division and character authority. Resolve only
the live population and pose here; do not open a nested character read door.
================
*/
func (rt *Runtime) SpawnQuestGuardian(division string, c *enterworld.Character) bool {
	if rt.Monsters == nil || c == nil || !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 {
		return false
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		return false
	}
	now := rt.Now().UnixMilli()
	return rt.Monsters.SpawnQuestGuardian(simulation.QuestMonsterSpawn{
		Division: division, Population: lease, Codename: "MOB_QT_02_PUNISHER_CLON", NowMs: now,
		Position: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now),
	})
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
	remaining := consumeItemUseRow(c, use.row)
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
	viewer := skillobject.Viewer{Division: division, Population: lease, Position: worldgeom.RegionXZ{
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
before a trap can dispatch a new capture. Publication follows all retirements.
================
*/
func (rt *Runtime) AdvanceSkillObjects(nowMs int64, sessions []simulation.SessionSnapshot) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, object := range rt.SkillObjects.Snapshot() {
		out = append(out, rt.advanceSkillObject(object, nowMs)...)
	}
	objects := rt.SkillObjects.Snapshot()
	for _, session := range sessions {
		pose := session.World.LiveSpawnAt(nowMs)
		frames := skillobject.ScopeFrames(objects, skillobject.Viewer{
			Division: session.DivisionID, Population: session.Population, Published: session.PublishedObjects,
			Position: worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z},
		})
		if len(frames) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: session.DivisionID,
				OnlyCharacterID: session.CharacterID, Frames: simFrames(frames)})
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
	_, targetGID, retired := rt.SkillObjects.Scan(object.Spawn.GID, nowMs, ownerPresent, targets)
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
