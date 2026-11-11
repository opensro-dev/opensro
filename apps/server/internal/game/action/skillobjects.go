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
	"slices"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
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

Natively the special consumable begins the indirect skill at its owner. The item debit
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

A trap's kill commits its planter's EXP here, so the pass is a tick step
like TickHook's (#713): it holds the publication gate over its commits and
seals their private tails to the recipients captured at commit time. The
tick's own session list holds only world-ready sessions; without the seal
a planter whose client is loading lost its 0x30D2 while the server kept
the EXP, and the client's next level crossing failed its length check.
The wait for that gate is the tick gate's bounded one (#2294): a pass
cut by a held gate commits nothing and reruns whole on the next tick.
================
*/
func (rt *Runtime) AdvanceSkillObjects(nowMs int64, sessions []simulation.SessionView) []simulation.DivisionFrames {
	var out, results []simulation.DivisionFrames
	before, generation := rt.advanceSkillObjectCommits(nowMs, &out, &results)
	// A commit pass that wrote nothing leaves the registry exactly as
	// before names it, so the post-pass copy is paid only on a pass that
	// created, retired or restamped an object.
	after := before
	settled := rt.SkillObjects.Generation() == generation
	if !settled {
		after = rt.SkillObjects.Snapshot()
	}
	for _, session := range sessions {
		if session.PublishedObjects == nil {
			continue
		}
		pose := session.World.LiveSpawnAt(nowMs)
		position := worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}
		if settled && rt.skillObjectScopeSettled(session, generation, position) {
			continue
		}
		viewer := skillobject.Viewer{
			Division: session.DivisionID, Population: session.Population, Published: session.PublishedObjects,
			CharacterGID: simulation.PlayerObjectID(session.CharacterID),
			Position:     position,
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
			// Frames are outstanding until transport admits them into
			// the session's published set; the next pass recomputes
			// rather than trusting a mark over unadmitted scope.
			rt.skillObjectScopeForget(session.SessionID)
		} else if settled {
			rt.skillObjectScopeSettle(session, generation, position)
		}
	}
	rt.skillObjectScopePrune(sessions)
	return out
}

/*
================
skillObjectScopeMark

One viewer's settled scope inputs (#3101): the registry generation,
the viewer's division, identity and population, its pose and the
published set a scope pass last found settled - it computed no frames.
The scope frames are a pure function of those inputs (ScopeFrames
over the registry the generation names), so while they all stand the
pass is empty and the per-object geometry walk is skipped.
================
*/
type skillObjectScopeMark struct {
	generation  uint64
	division    string
	characterID int64
	population  instance.Lease
	position    worldgeom.RegionXZ
	published   []uint32
}

/*
================
skillObjectScopeSettled

Whether the viewer's settled pass ran on exactly these inputs.
================
*/
func (rt *Runtime) skillObjectScopeSettled(session simulation.SessionView, generation uint64, position worldgeom.RegionXZ) bool {
	rt.skillObjectScopeMu.Lock()
	defer rt.skillObjectScopeMu.Unlock()
	mark, ok := rt.skillObjectScopeMarks[session.SessionID]
	return ok && mark.generation == generation && mark.division == session.DivisionID &&
		mark.characterID == session.CharacterID && mark.population == session.Population &&
		mark.position == position && slices.Equal(mark.published, session.PublishedObjects)
}

/*
================
skillObjectScopeSettle

Record a scope pass that computed no frames, keeping a copy of the
published set it settled on.
================
*/
func (rt *Runtime) skillObjectScopeSettle(session simulation.SessionView, generation uint64, position worldgeom.RegionXZ) {
	rt.skillObjectScopeMu.Lock()
	defer rt.skillObjectScopeMu.Unlock()
	if rt.skillObjectScopeMarks == nil {
		rt.skillObjectScopeMarks = map[string]skillObjectScopeMark{}
	}
	mark := rt.skillObjectScopeMarks[session.SessionID]
	mark.generation, mark.division = generation, session.DivisionID
	mark.characterID, mark.population, mark.position = session.CharacterID, session.Population, position
	mark.published = append(mark.published[:0], session.PublishedObjects...)
	rt.skillObjectScopeMarks[session.SessionID] = mark
}

/*
================
skillObjectScopeForget
================
*/
func (rt *Runtime) skillObjectScopeForget(sessionID string) {
	rt.skillObjectScopeMu.Lock()
	defer rt.skillObjectScopeMu.Unlock()
	delete(rt.skillObjectScopeMarks, sessionID)
}

/*
================
skillObjectScopePrune

Drop the marks of sessions this pass did not see, so the table holds
only live sessions.
================
*/
func (rt *Runtime) skillObjectScopePrune(sessions []simulation.SessionView) {
	rt.skillObjectScopeMu.Lock()
	defer rt.skillObjectScopeMu.Unlock()
	if len(rt.skillObjectScopeMarks) == 0 {
		return
	}
	seen := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		seen[session.SessionID] = true
	}
	for sessionID := range rt.skillObjectScopeMarks {
		if !seen[sessionID] {
			delete(rt.skillObjectScopeMarks, sessionID)
		}
	}
}

/*
================
advanceSkillObjectCommits

Runs every live object's pulse under the publication gate and returns the
objects as they stood before it, with the registry generation read just
before that copy. A batch sourced by its own object is a
viewer row (results); every other batch is the pulse's routed output, whose
private tails are sealed before the gate is released.
================
*/
func (rt *Runtime) advanceSkillObjectCommits(nowMs int64, out, results *[]simulation.DivisionFrames) ([]skillobject.Object, uint64) {
	if !rt.acquireTickGate() {
		// The gate is held (an entry bootstrap marshalling its snapshot,
		// say): the pass waits only within the tick gate's bound (#2294).
		// Nothing is lost - every due time below is absolute, so fields,
		// trap pulses, scans and retirements read the same state on the
		// next served pass; and committing nothing is exactly what the
		// gate orders against the holder's snapshot. The registry's own
		// lock makes the before copy safe without the gate.
		generation := rt.SkillObjects.Generation()
		return rt.SkillObjects.Snapshot(), generation
	}
	defer rt.releaseTickGate()
	generation := rt.SkillObjects.Generation()
	before := rt.SkillObjects.Snapshot()
	var routed []simulation.DivisionFrames
	for _, object := range before {
		if rt.skillObjectWaiting(object, nowMs) {
			continue
		}
		for _, batch := range rt.advanceSkillObject(object, nowMs) {
			if batch.SourceGID == object.Spawn.GID {
				*results = append(*results, batch)
			} else {
				routed = append(routed, batch)
			}
		}
	}
	*out = append(*out, rt.sealTickPrivate(routed)...)
	return before, generation
}

/*
================
skillObjectOwnerAdmit

The owner-presence verdict of one object: the facts the admitted
copy is weighed on, shared by the waiting probe below and the copy
in advanceSkillObject so the two can never disagree.
================
*/
func skillObjectOwnerAdmit(object skillobject.Object) func(*enterworld.Character) bool {
	return func(live *enterworld.Character) bool {
		return !live.DeletePending && enterworld.CharacterAlive(live) &&
			live.NativeTeleportMode == 0 &&
			enterworld.ObjectIDForCharacter(live) == object.OwnerGID &&
			domain.CharacterWorldInstance(live) == uint32(object.Population.ID)
	}
}

/*
================
skillObjectWaiting

Whether the object cannot change this tick (#3101). The registry
weighs expiry and capture in the scan itself (Scan/Advance), and the
scan only runs at the object's deadline, so an object whose deadline
has not come and whose duration has not run out can only change
through its owner: the population lease going, or the presence
verdict turning. Both are probed here on their owners' terms - the
lease index, and the presence verdict on the live record
(characterAdmitted, no copy) - so an owner who dies, teleports or
leaves still retires the object on this tick's full pass, while a
waiting object stays off the division lock, the admitted character
copy and the monster candidate walk until its deadline. One
retirement does move to the deadline: a combat trap's owner-distance
retirement is weighed from the owner's pose inside the scan pass,
which is the cadence its tests advance by (CombatTrapScanMs).
================
*/
func (rt *Runtime) skillObjectWaiting(object skillobject.Object, nowMs int64) bool {
	if nowMs >= object.NextScanMs || nowMs-object.CreatedMs > int64(object.Program.DurationMs) {
		return false
	}
	lease, present := rt.EntryPopulationLease(object.Division, object.OwnerName)
	if !present || lease != object.Population {
		return false
	}
	c := rt.findCharacter(object.Division, object.OwnerName)
	return rt.characterAdmitted(object.Division, c, skillObjectOwnerAdmit(object))
}

/*
================
advanceSkillObject

Natively the monster AI's first opponent (+BC) is checked, not its summoner or
loot owner. A matching trap retires before the quest handler rolls capture.
================
*/
func (rt *Runtime) advanceSkillObject(object skillobject.Object, nowMs int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(object.Division)
	defer unlock()
	c := rt.findCharacter(object.Division, object.OwnerName)
	lease, present := rt.EntryPopulationLease(object.Division, object.OwnerName)
	// The presence verdict weighs only record facts the copy carries
	// verbatim, so the copy is admitted on those facts: an absent
	// owner retires its object in the registry before any reader
	// touches the snapshot (#1943). The copy is the combat copy:
	// every reader below weighs the owner's name, GID, pose, world
	// instance, guild and combat stats - victim selection, the field
	// and trap strikes and the trap explosion read nothing from the
	// bag and resolve none of the owner's companions (the two
	// downstream re-copies, screamMaskPlayer's attacker view and
	// returnDamageToPlayer's attacker, read only stats, pose and
	// GID of theirs).
	var snapshot *enterworld.Character
	if present && lease == object.Population {
		snapshot = rt.characterSnapshotCombatAdmitted(object.Division, c, skillObjectOwnerAdmit(object))
	}
	ownerPresent := snapshot != nil
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
		// Only a monster triggers (natively its AI's first opponent is read); the
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
	rt.deps.UpdateDeferred(c, "quest-trap-capture", func() bool {
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
