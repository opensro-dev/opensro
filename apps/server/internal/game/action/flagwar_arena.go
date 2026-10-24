/*
===========================================================================

flagwar_arena.go - the Capture the Flag arena: objects, touches, combat

PR 2 of the flag war (#887), arena half. flagwar_match.go owns the
match lifecycle (start, tick, end); this file owns everything that
stands in the arena and everything a player's click or sword does to
it: the side monsters, the match-owned NPC-band objects (poles,
dropped flag, master keys, doors) and their create rows, the pole
channels' targets (the touch handlers a click lands on), and the
combat seams (arena hostility and the fatal-blow hook).

The native picture and the port choices live in the flagwar_match.go
header; the two facts a reader of this file needs first: the match
book is guarded by match.mu (lock order: division lane, then the
match lock - see flagwarMatch), and a flag fall resets every pole
channel, the way the native drop rewrites the three channel triples.

===========================================================================
*/

package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
flagWarSpawnSides

Ten monsters per side around the side anchor; one per side carries
the key. PORT CHOICE: the scatter is a deterministic ring of radius
60..240 (native scatters randomly within 300), and the key carrier is
a fixed pick of the ten (native rolls rand%10). PORT INFERENCE: dead
side monsters do not respawn (every arena monster carries an on-death
handler whose effect was not read).
================
*/
func (rt *Runtime) flagWarSpawnSides(match *flagWarMatch, levels []int64, nowMs int64) {
	if rt.Monsters == nil {
		return
	}
	bracket := flagWarBracketFor(levels)
	ref, ok := rt.Monsters.ReferenceByCodename(flagWarBracketCodenames[bracket])
	if !ok {
		return
	}
	for side := 0; side < 2; side++ {
		anchor := flagWarMonsterAnchors[side]
		gids := make([]uint32, 0, flagWarMonstersPerSide)
		for index := 0; index < flagWarMonstersPerSide; index++ {
			// Golden-angle ring: deterministic, inside the native radius.
			angle := float64(index) * 2.39996
			radius := 60.0 + float64(index%4)*60.0
			position := simulation.Spawn{
				RegionID: anchor.RegionID,
				X:        anchor.X + radius*math.Sin(angle),
				Y:        anchor.Y,
				Z:        anchor.Z + radius*math.Cos(angle),
			}
			spawned := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{
				Division:   match.division,
				Population: match.lease,
				RefObjID:   ref.RefObjID,
				Count:      1,
				Type:       0,
				Position:   position,
				NowMs:      nowMs,
			})
			gids = append(gids, spawned...)
		}
		if len(gids) > 0 {
			match.keyMonsterGid[side] = gids[len(gids)/2]
			match.keyLastSpawn[side] = anchor
		}
	}
}

/*
================
flagWarBuildObjects

The arena objects: three poles (centre flying) and the two doors.
The doors stand by their side's spawn (PORT CHOICE for placement; the
native door positions were not pinned).
================
*/
func (rt *Runtime) flagWarBuildObjects(match *flagWarMatch) {
	for index := 0; index < 3; index++ {
		plain := 0
		if match.poles[index] {
			plain = 1
		}
		codename := flagWarPoleCodenames[index][plain]
		object := &flagWarArenaObject{
			gid:       flagWarObjectGID(flagWarSlotPole0 + index),
			kind:      flagWarObjectPole,
			poleIndex: index,
			side:      flagWarPoleSides[index],
			codename:  codename,
			spawn:     flagWarPoleSpawns[index],
			live:      true,
		}
		if ref, ok := rt.flagWarReference(codename); ok {
			object.refObjID = ref
		}
		match.objects[object.gid] = object
	}
	for side := 0; side < 2; side++ {
		spawn := flagWarTeamSpawns[side]
		spawn.X += 24 // beside the spawn, off the landing spot
		codename := flagWarDoorCodenames[side]
		object := &flagWarArenaObject{
			gid:      flagWarObjectGID(flagWarSlotDoor0 + side),
			kind:     flagWarObjectDoor,
			side:     side,
			codename: codename,
			spawn:    spawn,
			live:     true,
		}
		if ref, ok := rt.flagWarReference(codename); ok {
			object.refObjID = ref
		}
		match.objects[object.gid] = object
	}
}

// flagWarCharacterIDs is the raw characterdata codename index
// (enterworld.TextdataItems).
type flagWarCharacterIDs interface {
	CharacterRefObjIDByCodename(codename string) (uint32, bool)
}

/*
================
flagWarReference

The RefObjID for an arena codename. The poles, flag, key and doors are
static NPC-class character rows: the monster template leaves them out and
CharacterRefByCodename refuses them (no movement columns), so the raw
characterdata index answers first; the monster template stays as the
fallback.
================
*/
func (rt *Runtime) flagWarReference(codename string) (uint32, bool) {
	if refs, ok := rt.deps.ItemReferences().(flagWarCharacterIDs); ok {
		if id, found := refs.CharacterRefObjIDByCodename(codename); found {
			return id, true
		}
	}
	if rt.Monsters == nil {
		return 0, false
	}
	ref, ok := rt.Monsters.ReferenceByCodename(codename)
	if !ok {
		return 0, false
	}
	return ref.RefObjID, true
}

/*
================
sortedObjects

The arena objects in gid order: create-row order is deterministic.
================
*/
func (m *flagWarMatch) sortedObjects() []*flagWarArenaObject {
	gids := make([]uint32, 0, len(m.objects))
	for gid := range m.objects {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	out := make([]*flagWarArenaObject, 0, len(gids))
	for _, gid := range gids {
		out = append(out, m.objects[gid])
	}
	return out
}

/*
================
flagWarObjectRow

The create row for one arena object, published on the object-list
chunk (0x3417) exactly the way roster rows enter.
================
*/
func (rt *Runtime) flagWarObjectRow(o *flagWarArenaObject) []byte {
	def := simulation.NpcDef{
		RefObjID:      o.refObjID,
		ObjectID:      o.gid,
		Codename:      o.codename,
		Name:          rt.flagWarRefName(o.codename, o.codename),
		AuthoredSpawn: true,
		Spawn:         o.spawn,
	}
	return simulation.BuildNpcCreateRow(def, o.spawn)
}

/*
================
flagWarObjectDef

The def a despawn for one arena object names.
================
*/
func (rt *Runtime) flagWarObjectDef(o *flagWarArenaObject) simulation.NpcDef {
	return simulation.NpcDef{
		RefObjID:      o.refObjID,
		ObjectID:      o.gid,
		Codename:      o.codename,
		Name:          rt.flagWarRefName(o.codename, o.codename),
		AuthoredSpawn: true,
		Spawn:         o.spawn,
	}
}

/*
================
flagWarRefName

The display name for an arena codename from the authored reference
data. Names are cosmetic on the wire row; the codename stands in when
no reference row exists.
================
*/
func (rt *Runtime) flagWarRefName(codename, fallback string) string {
	if rt.Monsters == nil {
		return fallback
	}
	if ref, ok := rt.Monsters.ReferenceByCodename(codename); ok && ref.Name != "" {
		return ref.Name
	}
	return fallback
}

/*
================
flagWarSwapPole

Republish one pole in its new shape: despawn the old row, create the
new one, for every entered participant.
================
*/
func (rt *Runtime) flagWarSwapPole(match *flagWarMatch, pole int) []simulation.DivisionFrames {
	object := match.objects[flagWarObjectGID(flagWarSlotPole0+pole)]
	if object == nil {
		return nil
	}
	oldDef := rt.flagWarObjectDef(object)
	plain := 0
	if match.poles[pole] {
		plain = 1
	}
	object.codename = flagWarPoleCodenames[pole][plain]
	if ref, ok := rt.flagWarReference(object.codename); ok {
		object.refObjID = ref
	}
	var out []simulation.DivisionFrames
	for _, id := range match.order {
		p := match.participants[id]
		if p == nil || !p.entered {
			continue
		}
		for _, frame := range simulation.NpcDespawnFrames([]simulation.NpcDef{oldDef}) {
			out = append(out, simulation.DivisionFrames{
				DivisionID:      p.registrant.Division,
				OnlyCharacterID: p.registrant.CharacterID,
				Frames:          []simulation.Frame{frame},
			})
		}
		out = append(out, flagWarParticipantFrames(p, wire.OpObjectListChunk, rt.flagWarObjectRow(object)))
	}
	return out
}

/*
================
flagWarDropFlag

The flag falls: the dropped-flag NPC stands at the spot and the drop
notice goes to the whole match.
================
*/
func (rt *Runtime) flagWarDropFlag(match *flagWarMatch, at simulation.Spawn) []simulation.DivisionFrames {
	match.carrierID = 0
	// The native drop resets every pole channel (the drop handler
	// rewrites all three channel triples in the world record): a
	// channel that outlives the fall must never complete after it.
	for pole := range match.channels {
		match.channels[pole] = nil
	}
	if match.flagLive {
		return nil
	}
	match.flagLive = true
	match.flagSpawn = at
	object := &flagWarArenaObject{
		gid:      flagWarObjectGID(flagWarSlotFlag),
		kind:     flagWarObjectFlag,
		side:     -1,
		codename: flagWarCodenameFlagNPC,
		spawn:    at,
		live:     true,
	}
	if ref, ok := rt.flagWarReference(object.codename); ok {
		object.refObjID = ref
	}
	match.objects[object.gid] = object
	var out []simulation.DivisionFrames
	for _, id := range match.order {
		p := match.participants[id]
		if p == nil || !p.entered {
			continue
		}
		out = append(out, flagWarParticipantFrames(p, wire.OpObjectListChunk, rt.flagWarObjectRow(object)))
	}
	out = append(out, match.allFrames(wire.OpFlagWarEvent,
		wire.EncodeFlagWarEventSubtype(wire.FlagWarFlagDrop))...)
	return out
}

/*
================
flagWarTickKeys

Watch the two key carriers: a dead carrier's key NPC rises at its
last live pose, once per side.
================
*/
func (rt *Runtime) flagWarTickKeys(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	var out []simulation.DivisionFrames
	for side := 0; side < 2; side++ {
		gid := match.keyMonsterGid[side]
		if gid == 0 || match.keyGone[side] {
			continue
		}
		mover, exists := rt.Monsters.Mover(match.division, gid)
		if exists {
			pose := mover.LivePoseAt(nowMs, nil)
			match.keyLastSpawn[side] = simulation.Spawn{
				RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z,
			}
			continue
		}
		match.keyGone[side] = true
		object := &flagWarArenaObject{
			gid:      flagWarObjectGID(flagWarSlotKey0 + side),
			kind:     flagWarObjectKey,
			side:     side,
			codename: flagWarCodenameMasterKeyNPC,
			spawn:    match.keyLastSpawn[side],
			live:     true,
		}
		if ref, ok := rt.flagWarReference(object.codename); ok {
			object.refObjID = ref
		}
		match.objects[object.gid] = object
		for _, id := range match.order {
			p := match.participants[id]
			if p == nil || !p.entered {
				continue
			}
			out = append(out, flagWarParticipantFrames(p, wire.OpObjectListChunk, rt.flagWarObjectRow(object)))
		}
	}
	return out
}

/*
================
flagWarTickCarrier

Sixty seconds (0xEA60) with the flag and it falls where the carrier
stands.
================
*/
func (rt *Runtime) flagWarTickCarrier(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	if match.carrierID == 0 || nowMs-match.carrierSinceMs < flagWarCarrierDropMs {
		return nil
	}
	carrier := match.participants[match.carrierID]
	if carrier == nil || !carrier.entered {
		match.carrierID = 0
		return nil
	}
	at := carrier.lastSpawn
	if !carrier.hasPose {
		at = flagWarTeamSpawns[carrier.team]
	}
	return rt.flagWarDropFlag(match, at)
}

/*
================
flagWarSelect

A click on a match object. The handler consults the match book
BEFORE roster resolution: these gids exist on no roster, so nothing
else can shadow them in practice. The answer is the ordinary NPC
grant (len-11 shape, no capabilities, no event byte: these objects
open no talk); the match rules run on top. Beyond the interaction
range the answer is the native too-far refusal, exactly like a
roster NPC - the client walks in and the next click lands.

Runs under the caller's division lane; the match lock nests inside
it (lane, then match - the order every match reader keeps).
================
*/
func (rt *Runtime) flagWarSelect(divisionID string, character *enterworld.Character, gid uint32) (SelectOutcome, bool) {
	match := rt.flagWarMatchFor(divisionID)
	if match == nil {
		return SelectOutcome{}, false
	}
	match.mu.Lock()
	defer match.mu.Unlock()
	object := match.objects[gid]
	if object == nil || !object.live {
		return SelectOutcome{}, false
	}
	nowMs := rt.Now().UnixMilli()
	pose := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, nowMs)
	dx := pose.X - object.spawn.X
	dz := pose.Z - object.spawn.Z
	if dx*dx+dz*dz > flagWarChannelRange*flagWarChannelRange {
		return SelectOutcome{
			Refusal: "flag war object beyond interaction range",
			Frames: []wire.Frame{{
				Opcode:  wire.OpObjectSelectResult,
				Payload: wire.EncodeObjectSelectRefusal(hitRangeTooFar),
			}},
		}, true
	}
	rt.Selected.Set(divisionID, character.Name, gid)
	rt.NpcDialogs.Clear(divisionID, character.Name)
	outcome := SelectOutcome{
		Selected: gid,
		Frames: []wire.Frame{{
			Opcode:  wire.OpObjectSelectResult,
			Payload: wire.EncodeNpcObjectSelectResult(gid, 0, 0),
		}},
	}
	participant := match.participants[character.ID]
	if participant == nil || !participant.entered {
		return outcome, true
	}
	switch object.kind {
	case flagWarObjectPole:
		outcome.Frames = append(outcome.Frames,
			rt.flagWarTouchPole(match, object, participant, pose, nowMs)...)
	case flagWarObjectFlag:
		rt.flagWarTouchFlag(match, object, participant, nowMs)
	case flagWarObjectKey:
		rt.flagWarTouchKey(match, object, participant)
	}
	return outcome, true
}

/*
================
flagWarTouchPole

Steal a flying pole the clicker's side may take, or score on the
clicker's own plain pole while carrying. A busy pole answers the
busy subtype for the attempted mode; wrong side, wrong state, or
already carrying answers nothing.
================
*/
func (rt *Runtime) flagWarTouchPole(match *flagWarMatch, object *flagWarArenaObject, participant *flagWarParticipant, pose simulation.Spawn, nowMs int64) []wire.Frame {
	pole := object.poleIndex
	busy := func(subtype uint8) []wire.Frame {
		return []wire.Frame{{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarEventSubtype(subtype),
		}}
	}
	if match.poles[pole] {
		// Flying: steal. The centre pole is open to both sides; a team
		// pole only to the enemy side.
		if object.side >= 0 && object.side == participant.team {
			return nil
		}
		if match.channels[pole] != nil {
			return busy(wire.FlagWarStealBusy)
		}
		if match.carrierID == participant.registrant.CharacterID {
			return nil // already carrying
		}
		// Single-flag rule: while a carrier carries or the dropped
		// flag stands, no new steal channel opens (its completion
		// could not grant the carry anyway).
		if match.carrierID != 0 || match.flagLive {
			return nil
		}
		match.channels[pole] = &flagWarChannel{
			characterID: participant.registrant.CharacterID,
			gid:         participant.registrant.Gid,
			name:        participant.registrant.Name,
			team:        participant.team,
			mode:        1,
			deadlineMs:  nowMs + flagWarChannelMs,
			startX:      pose.X,
			startZ:      pose.Z,
		}
		// At steal start every participant sees the channeler marked.
		for _, id := range match.order {
			other := match.participants[id]
			if other == nil || !other.entered || other == participant {
				continue
			}
			rt.flagWarPush(match.division, other.registrant.Name, simulation.Frame{
				Opcode:  wire.OpFlagWarEvent,
				Payload: wire.EncodeFlagWarCarrier(participant.registrant.Gid),
			})
		}
		return []wire.Frame{{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarCarrier(participant.registrant.Gid),
		}}
	}
	// Plain: score, only the carrier on their own pole.
	if object.side < 0 || object.side != participant.team {
		return nil
	}
	if match.carrierID != participant.registrant.CharacterID {
		return nil
	}
	if match.channels[pole] != nil {
		return busy(wire.FlagWarScoreBusy)
	}
	match.channels[pole] = &flagWarChannel{
		characterID: participant.registrant.CharacterID,
		gid:         participant.registrant.Gid,
		name:        participant.registrant.Name,
		team:        participant.team,
		mode:        0,
		deadlineMs:  nowMs + flagWarChannelMs,
		startX:      pose.X,
		startZ:      pose.Z,
	}
	return nil
}

/*
================
flagWarTouchFlag

Clicking the dropped flag makes the clicker the carrier.
================
*/
func (rt *Runtime) flagWarTouchFlag(match *flagWarMatch, object *flagWarArenaObject, participant *flagWarParticipant, nowMs int64) {
	// Single-flag rule: a live carrier means this flag should not
	// stand at all; never mint a second carrier from it.
	if match.carrierID != 0 {
		return
	}
	object.live = false
	match.flagLive = false
	// A pickup retires any steal channel still running: the carry
	// just changed hands, so no channel may complete into it.
	for pole := range match.channels {
		match.channels[pole] = nil
	}
	match.carrierID = participant.registrant.CharacterID
	match.carrierSinceMs = nowMs
	def := rt.flagWarObjectDef(object)
	for _, id := range match.order {
		other := match.participants[id]
		if other == nil || !other.entered {
			continue
		}
		for _, frame := range simulation.NpcDespawnFrames([]simulation.NpcDef{def}) {
			rt.flagWarPush(match.division, other.registrant.Name, frame)
		}
		rt.flagWarPush(match.division, other.registrant.Name, simulation.Frame{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarCarrier(participant.registrant.Gid),
		})
	}
}

/*
================
flagWarTouchKey

Clicking a side's key NPC banks that side's master key and tells the
whole match (the client notices only its own side).
================
*/
func (rt *Runtime) flagWarTouchKey(match *flagWarMatch, object *flagWarArenaObject, participant *flagWarParticipant) {
	object.live = false
	// Native (the key-click handler, checked at HLIL and MLIL): the
	// key banks for the CLICKER's side and the broadcast team byte
	// is the clicker's side too - the dead carrier's side is never
	// consulted, so the click is not side-gated. The port despawns
	// the NPC on the click (one corpse, one claim).
	match.keyTaken[participant.team] = true
	def := rt.flagWarObjectDef(object)
	for _, id := range match.order {
		other := match.participants[id]
		if other == nil || !other.entered {
			continue
		}
		for _, frame := range simulation.NpcDespawnFrames([]simulation.NpcDef{def}) {
			rt.flagWarPush(match.division, other.registrant.Name, frame)
		}
		rt.flagWarPush(match.division, other.registrant.Name, simulation.Frame{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarMasterKey(object.gid, uint8(participant.team), participant.registrant.Name),
		})
	}
}

/*
================
flagWarHostile

The native event-world hostility: two entered participants of the
same live match on opposite teams are enemies.
================
*/
func (rt *Runtime) flagWarHostile(division string, a, b *enterworld.Character) bool {
	if a == nil || b == nil {
		return false
	}
	match := rt.flagWarMatchFor(division)
	if match == nil {
		return false
	}
	match.mu.Lock()
	defer match.mu.Unlock()
	pa := match.participants[a.ID]
	pb := match.participants[b.ID]
	if pa == nil || pb == nil || !pa.entered || !pb.entered {
		return false
	}
	return pa.team != pb.team
}

/*
================
recordFlagWarDeath

The fatal-blow hook, beside the fortress one. A dying carrier drops
the flag where they fell and loses their channel; an opposing
participant who lands the kill within nine levels of the carrier
takes the killer's trophy.
================
*/
func (rt *Runtime) recordFlagWarDeath(division string, victim, killer *enterworld.Character, nowMs int64) {
	match := rt.flagWarMatchFor(division)
	if match == nil || victim == nil {
		return
	}
	// The fatal-hit publish calls this under the division lane; the
	// match lock nests inside it. Nothing below may take the lane:
	// the killer's trophy is queued for the tick, not minted here.
	match.mu.Lock()
	defer match.mu.Unlock()
	dead := match.participants[victim.ID]
	if dead == nil || !dead.entered {
		return
	}
	for pole := 0; pole < 3; pole++ {
		if ch := match.channels[pole]; ch != nil && ch.characterID == victim.ID {
			match.channels[pole] = nil
		}
	}
	if match.carrierID != victim.ID {
		return
	}
	match.carrierID = 0
	pose := rt.liveSpawn(simulation.WorldKey(division, victim.Name), victim, nowMs)
	for _, batch := range rt.flagWarDropFlag(match, pose) {
		p := match.participants[batch.OnlyCharacterID]
		if p == nil {
			continue
		}
		for _, frame := range batch.Frames {
			rt.flagWarPush(division, p.registrant.Name, frame)
		}
	}
	if killer == nil {
		return
	}
	hunter := match.participants[killer.ID]
	if hunter == nil || !hunter.entered || hunter.team == dead.team {
		return
	}
	// The window reads both levels live at the death (the native
	// reads the two level bytes there; arena monsters pay
	// experience, so the match-start snapshot can drift).
	diff := flagWarLevelOf(killer.Level) - flagWarLevelOf(victim.Level)
	if diff < 0 {
		diff = -diff
	}
	if diff <= flagWarKeyLevelWindow {
		match.pendingMints = append(match.pendingMints, hunter.registrant.Name)
	}
}
