/*
===========================================================================

rebirth.go - revival and return-point resolution

===========================================================================
*/

package action

import (
	"math"
	"reflect"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
worldSpawnFromMission
================
*/
func worldSpawnFromMission(spawn simulation.Spawn) *enterworld.WorldSpawn {
	regionID := int64(spawn.RegionID)
	x, y, z := spawn.X, spawn.Y, spawn.Z
	angle := int64(spawn.Angle)
	return &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle}
}

/*
================
missionSpawnFromWorld
================
*/
func missionSpawnFromWorld(spawn *enterworld.WorldSpawn, fallback simulation.Spawn) simulation.Spawn {
	if spawn == nil || spawn.RegionID == nil || *spawn.RegionID <= 0 || *spawn.RegionID > 0xffff {
		return fallback
	}
	result := fallback
	result.RegionID = uint16(*spawn.RegionID)
	if spawn.X != nil {
		result.X = *spawn.X
	}
	if spawn.Y != nil {
		result.Y = *spawn.Y
	}
	if spawn.Z != nil {
		result.Z = *spawn.Z
	}
	if spawn.Angle != nil {
		result.Angle = uint16(*spawn.Angle & 0xffff)
	}
	return simulation.NormalizeSpawnFrame(result)
}

/*
================
defaultRebirthPoint
================
*/
func defaultRebirthPoint(character *enterworld.Character) simulation.Spawn {
	if character != nil && strings.HasPrefix(character.ModelCodename, "CHAR_EU") {
		return simulation.EuropeStartProfile()
	}
	return simulation.ChinaStartProfile()
}

/*
==================
appointedRebirthPoint

Both death and return scrolls resolve the appointed reference through the
current authored catalog (research server 4E08A0 -> 407760). Stored coordinates
are the fallback, so old saves and subsequently removed gates remain usable.
==================
*/
func (rt *Runtime) appointedRebirthPoint(character *enterworld.Character) simulation.Spawn {
	fallback := defaultRebirthPoint(character)
	if character == nil || character.World == nil {
		return fallback
	}
	world := character.World
	if rt.portals != nil && world.RebirthGateRefID != 0 {
		if id, ok := rt.portals.sources[world.RebirthGateRefID]; ok {
			d := rt.portals.destinations[id]
			if d.recall && d.spawn.RegionID != 0 && d.spawn.RegionID&0x8000 == 0 {
				return d.spawn
			}
		}
	}
	return missionSpawnFromWorld(world.RebirthPoint, fallback)
}

/*
================
missionReentryFrames
================
*/
func missionReentryFrames(packets []enterworld.Packet) []wire.Frame {
	frames := make([]wire.Frame, 0, len(packets))
	for _, packet := range packets {
		payload := make([]byte, len(packet.Payload))
		for index, value := range packet.Payload {
			payload[index] = byte(value)
		}
		frames = append(frames, wire.Frame{
			Scope:   packet.Scope,
			Opcode:  packet.NativeOpcode,
			Payload: payload,
		})
	}
	return frames
}

/*
==================
HandleRebirthPointAppointment

HandleRebirthPointAppointment consumes native 0x720D [selectedNpcGid].
Only a live character's selected, nearby 0x40 NPC/gate can persist its town
return point; success answers the native 0xB20D [1] acknowledgement.
==================
*/
func (rt *Runtime) HandleRebirthPointAppointment(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if err != nil || r.Done() != nil || character == nil {
		return OpResult{}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	selected, selectedOK := rt.Selected.Get(divisionID, character.Name)
	npc, npcOK := rt.npcForCurrentViewer(divisionID, character, gid)
	if !selectedOK || selected != gid || !npcOK ||
		npc.TalkFlags&simulation.NpcTalkFlagRecallPoint == 0 || npc.RebirthPoint.RegionID == 0 {
		return OpResult{}
	}
	if npc.AuthoredSpawn {
		live := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
		distance := math.Hypot(simulation.WorldDistance2D(live, npc.Spawn), live.Y-npc.Spawn.Y)
		limit := 300.0
		if npc.Teleport != nil {
			limit = 800
		}
		if math.IsNaN(distance) || math.IsInf(distance, 0) || distance > limit {
			return OpResult{}
		}
	}
	if !rt.deps.Update(character, "rebirth-point-appoint", func() bool {
		if character.DeletePending || !enterworld.CharacterAlive(character) || character.NativeTeleportMode != 0 {
			return false
		}
		next := enterworld.CharacterWorld{}
		if character.World != nil {
			next = *character.World
		}
		next.RebirthPoint = worldSpawnFromMission(npc.RebirthPoint)
		next.RebirthGateRefID = npc.RefObjID
		character.World = &next
		return true
	}) {
		return OpResult{}
	}
	return OpResult{Frames: []wire.Frame{{
		Opcode: wire.OpRebirthPointAppointResult, Payload: []byte{1},
	}}}
}

/*
==================
HandleLocalRebirth

HandleLocalRebirth owns native 0x32DC self-rebirth. Choice 1 restores at
the persisted town point (or race start before one is appointed); choice 2
is the retail level<=10 present-position concession, adding one HP and forty
percent of the keeper maxima while retaining existing MP. World, HP and MP commit
before publication. A town rebirth changes residency and therefore owns a
reset/re-entry corpus; a present-position rebirth stays in the resident scene
and publishes only correction, vitals, and LIFE-alive. Peers receive the
correction and LIFE-alive pair in either case.
==================
*/
func (rt *Runtime) HandleLocalRebirth(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	choice, err := wire.DecodeLocalRebirthRequest(payload)
	if err != nil || character == nil {
		return OpResult{}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	// Prepare the complete destination projection while still dead. No durable
	// revival may depend on a later fallible packet build or correction fallback.
	var before *enterworld.Character
	var corpse simulation.WorldState
	worldKey := simulation.WorldKey(divisionID, character.Name)
	rt.deps.Read(divisionID, func() {
		before = character.Snapshot()
		corpse = rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	})
	if before.DeletePending || enterworld.CharacterAlive(before) || corpse.MoveSegment != nil {
		return OpResult{DiagnosticRefusal: "rebirth-requires-settled-corpse"}
	}
	if choice == wire.RebirthAtPresentPoint && before.Level != nil && *before.Level > 10 {
		return OpResult{}
	}
	destination := corpse.Spawn
	candidate := before.Snapshot()
	restoredHP, restoredMP, _, _ := rt.playerKeeperVitals(divisionID, candidate)
	if choice == wire.RebirthAtPresentPoint {
		restoredHP, restoredMP = rt.presentRebirthVitals(divisionID, candidate)
	}
	candidate.CurrentHP, candidate.CurrentMP = &restoredHP, &restoredMP
	var prepared enterworld.PreparedReentry
	var previousPets map[petOwnerKey]petSession
	committed := false
	defer func() {
		if !committed {
			rt.restoreCompanionRelocation(previousPets)
		}
	}()
	if choice == wire.RebirthAtSpecifiedPoint {
		destination = rt.appointedRebirthPoint(before)
		preview := corpse
		preview.Spawn = destination
		writeBackWorld(candidate, preview)
		previousPets = rt.relocateReturningPet(divisionID, character, destination)
		var ok bool
		prepared, ok = rt.deps.PrepareReentry(divisionID, candidate)
		if !ok || len(prepared.Packets) == 0 || prepared.Packets[0].NativeOpcode != enterworld.OpcodeResetClient {
			return OpResult{DiagnosticRefusal: "rebirth-entry-preparation-failed"}
		}
		destination = prepared.Spawn
	}
	var untouchable []wire.Frame
	accepted := rt.deps.Update(character, "local-rebirth", func() bool {
		current := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
		// Packet preparation occurs outside the store door. Refuse stale input;
		// never roll back over a concurrent character mutation.
		if !reflect.DeepEqual(character.Snapshot(), before) || !reflect.DeepEqual(current, corpse) {
			return false
		}
		state := rt.Worlds.Update(worldKey, func() simulation.WorldState { return corpse }, func(world *simulation.WorldState) {
			world.Spawn = destination
			world.MoveSegment = nil
			world.LifeRevision++
			world.Sitting = false
			world.PostureTransitionUntilMs = 0
			world.SpawnSet = true
			world.MovementSourceSeeded = true
		})
		writeBackWorld(character, state)
		character.World.MoveSegment = nil
		character.CurrentHP, character.CurrentMP = &restoredHP, &restoredMP
		character.LastExpLoss = 0 // CGObjPC_TeleportToTown 4DF2E8
		untouchable = rt.grantReviveUntouchable(divisionID, character, rt.Now().UnixMilli())
		return true
	})
	if !accepted {
		return OpResult{}
	}
	committed = true
	rt.bindResidentRegion(worldKey, rt.Now().UnixMilli())
	rt.ClearCombatIntent(divisionID, character.Name)

	gid := enterworld.ObjectIDForCharacter(character)
	correction, vitals, life := rebirthFrames(gid, destination, enterworld.BuildVitalsRefreshPayload(character))
	if choice == wire.RebirthAtPresentPoint {
		return OpResult{
			Frames:    append([]wire.Frame{correction, vitals, life}, untouchable...),
			Broadcast: append([]wire.Frame{correction, life}, untouchable...),
		}
	}
	rt.retireReturnForReentry(divisionID, character)
	frames := missionReentryFrames(prepared.Packets)
	// Re-entry reconstructs the client actor. A retained runtime body status
	// must be replayed after construction; rebirth does not blanket-clear it.
	if snapshot := rt.characterSnapshot(divisionID, character); snapshot != nil && snapshot.NativeBodyStatus != 0 {
		frames = append(frames, bodyStatusFrame(gid, snapshot.NativeBodyStatus))
	}
	frames = append(frames, life)
	return OpResult{
		Frames:    frames,
		Broadcast: append([]wire.Frame{correction, life}, untouchable...),
	}
}

/*
==================
rebirthFrames

The publication of a revival: the correction that seats the actor at
destination, the vitals it came back with, then LIFE-alive. The vitals
precede LIFE (lifePublicationTransaction owns that order).
==================
*/
func rebirthFrames(gid uint32, destination simulation.Spawn, vitalsPayload []byte) (correction, vitals, life wire.Frame) {
	correction = wire.Frame{
		Opcode: wire.OpObjectSourceCorrection,
		Payload: wire.ObjectSourceCorrection{
			Gid: gid,
			Position: wire.Position{
				RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y),
				Z: float32(destination.Z), Heading: destination.Angle,
			},
		}.Encode(),
	}
	vitals = wire.Frame{Opcode: enterworld.OpcodeVitalsUpdate, Payload: vitalsPayload}
	publication := beginRebirthLifePublication(gid)
	publication.publishRebirthVitals()
	restored := publication.publishLifeRestored()
	life = wire.Frame{Opcode: restored.opcode, Payload: restored.payload}
	return correction, vitals, life
}
