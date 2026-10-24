/*
===========================================================================

flagwar_match.go - the Capture the Flag match owner

PR 2 of the flag war (#887): the daily match itself. PR 1 shipped the
registration scheduler; this file is the rt.FlagWarMatchStart hook the
scheduler fires at the start instant, the match tick (channels, the
carrier, the elapsed sync, the ending), the arena-object interactions,
and the reward mints.

Native evidence (research/docs/reverse-engineering/server/
FlagWarServer.md checkpoints 2-4, client FlagWarClient.md checkpoint
1; the checkpoint-4 geometry was pinned at the decisive layer):

  - The arena is region 0x8009. Pole 0 (white, centre) stands at
    (992, 0, -340), pole 1 (red, side 0) at (1578, 0, 202), pole 2
    (blue, side 1) at (339, 0, 198). Team spawns: side 0 (2104, 0, -8),
    side 1 (-183, 0, 5). Each side fields ten monsters scattered
    within 300 of its anchor (side 0 (1314, 0, 255), side 1
    (607, 0, 200)); exactly one per side carries the master key: its
    death spawns the key NPC at the corpse.
  - The monster codename follows the match level bracket byte 0..6:
    MOB_EV_BEAR_25 (0,1), MOB_EV_FOX_45 (2), MOB_EV_RAVEN_60 (3),
    MOB_EV_PENGUIN_75 (4), MOB_EV_KANGAROO_85 (5), MOB_EV_JINN_95 (6).
    PORT INFERENCE (recorded, not a native fact): the port flies one
    bracket per match from the mean participant level,
    clamp((avg-20)/15, 0, 6).
  - Channels run five seconds; the carrier drops the flag after sixty
    seconds (0xEA60 = 60000 ms); the elapsed sync rides every ten
    seconds; the match clock is twenty minutes (client timer
    0x124F80 ms). The winner is the side whose team pole flies when
    the clock ends; the centre pole alone never wins.
  - Rewards: every winning-team player takes one
    ITEM_ETC_E070919_TROPHY (owner substitution for the v1.188 ice
    trophy, which does not exist in v1.150 data); the flag carrier's
    killer takes one when the two levels are within nine.

Port choices (labelled, reversible): teams split evenly alternating by
sign-up order; a match object (pole, dropped flag, master key, door)
is a match-owned NPC-band object whose clicks are intercepted before
roster resolution, because a click must arrive as an NPC interaction,
not a monster attack; dead participants leave through the normal town
rebirth (native revives at the team spawn - follow-up, no per-world
rebirth override exists in the port yet); the arena monsters scatter
on a deterministic ring instead of the native random scatter; dead
side monsters do not respawn.

===========================================================================
*/

package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// The arena region (FlagWarServer.md checkpoint 4: the pole records
// are keyed -1/0/1 against regioncode 0x8009's world-size anchor).
const flagWarArenaRegionID uint16 = 0x8009

// Arena geometry (native, checkpoint 4). Y is the authored height 0.
var (
	flagWarPoleSpawns = [3]simulation.Spawn{
		{RegionID: flagWarArenaRegionID, X: 992, Y: 0, Z: -340}, // 0 white, centre
		{RegionID: flagWarArenaRegionID, X: 1578, Y: 0, Z: 202}, // 1 red, side 0
		{RegionID: flagWarArenaRegionID, X: 339, Y: 0, Z: 198},  // 2 blue, side 1
	}
	flagWarTeamSpawns = [2]simulation.Spawn{
		{RegionID: flagWarArenaRegionID, X: 2104, Y: 0, Z: -8}, // side 0
		{RegionID: flagWarArenaRegionID, X: -183, Y: 0, Z: 5},  // side 1
	}
	flagWarMonsterAnchors = [2]simulation.Spawn{
		{RegionID: flagWarArenaRegionID, X: 1314, Y: 0, Z: 255}, // side 0
		{RegionID: flagWarArenaRegionID, X: 607, Y: 0, Z: 200},  // side 1
	}
)

// The pole side each pole defends: -1 centre, else the owning side.
var flagWarPoleSides = [3]int{-1, 0, 1}

// Arena object codenames (v1.150 data, ref ids 24135-24144).
const (
	flagWarCodenameMasterKeyNPC = "NPC_EVENT_FLAGWAR_MASTERKEY" // 24135
	flagWarCodenameFlagNPC      = "NPC_EVENT_FLAGWAR_FLAG"      // 24142
	flagWarCodenameDoorBlue     = "NPC_EVENT_FLAGWAR_DOOR_BLUE" // 24143
	flagWarCodenameDoorRed      = "NPC_EVENT_FLAGWAR_DOOR_RED"  // 24144
)

// Pole codenames by pole index, plain and flying (flap) shapes.
var flagWarPoleCodenames = [3][2]string{
	{"NPC_EVENT_FLAGWAR_FLAGPOLE_WHITE", "NPC_EVENT_FLAGWAR_FLAP_FLAGPOLE_WHITE"},
	{"NPC_EVENT_FLAGWAR_FLAGPOLE_RED", "NPC_EVENT_FLAGWAR_FLAP_FLAGPOLE_RED"},
	{"NPC_EVENT_FLAGWAR_FLAGPOLE_BLUE", "NPC_EVENT_FLAGWAR_FLAP_FLAGPOLE_BLUE"},
}

// The master-key door codename per side. The native door consumer was
// never found; the doors ship as static arena NPCs (faithful-minimal).
var flagWarDoorCodenames = [2]string{flagWarCodenameDoorRed, flagWarCodenameDoorBlue}

// The monster codename per level bracket 0..6 (native table).
var flagWarBracketCodenames = [7]string{
	"MOB_EV_BEAR_25",     // 0
	"MOB_EV_BEAR_25",     // 1
	"MOB_EV_FOX_45",      // 2
	"MOB_EV_RAVEN_60",    // 3
	"MOB_EV_PENGUIN_75",  // 4
	"MOB_EV_KANGAROO_85", // 5
	"MOB_EV_JINN_95",     // 6
}

// Match pacing (native values; see the file header).
const (
	flagWarMatchDurationMs   int64 = 20 * 60 * 1000
	flagWarChannelMs         int64 = 5 * 1000
	flagWarCarrierDropMs     int64 = 60000 // 0xEA60
	flagWarElapsedSyncMs     int64 = 10 * 1000
	flagWarCloseAfterEndMs   int64 = 60 * 1000
	flagWarDeadlineSlackMs   int64 = 5 * 60 * 1000
	flagWarPlayerCap               = 16
	flagWarMonstersPerSide         = 10
	flagWarChannelRange            = 15.0
	flagWarChannelMoveLeeway       = 5.0
	flagWarKeyLevelWindow    int64 = 9
	flagWarTrophyCodename          = "ITEM_ETC_E070919_TROPHY"
)

// Match object kinds (the interception set).
const (
	flagWarObjectPole uint8 = iota
	flagWarObjectFlag
	flagWarObjectKey
	flagWarObjectDoor
)

// Match object gid slots inside the NPC band. The roster uses a few
// hundred ids from the band base and the magic-pop / gate lanes sit at
// 240000+ and 250000+RefObjID; 90000 leaves every lane clear, and the
// interception never resolves through the roster anyway.
const flagWarObjectGIDBase = domain.NPCGIDBase + 90_000

const (
	flagWarSlotPole0 = 0
	flagWarSlotFlag  = 3
	flagWarSlotKey0  = 4
	flagWarSlotDoor0 = 6
)

/*
================
flagWarParticipant

One entered player: team, the level the bracket and the killer reward
read, and the last live pose the carrier drop and the channel leeway
measure.
================
*/
type flagWarParticipant struct {
	registrant FlagWarRegistrant
	team       int
	level      int64
	entered    bool
	lastSpawn  simulation.Spawn
	hasPose    bool
}

/*
================
flagWarChannel

One five-second pole channel. Mode 1 steals a flying pole, mode 0
scores on the channeler's own plain pole (the native channel-mode
byte).
================
*/
type flagWarChannel struct {
	characterID int64
	gid         uint32 // the channeler's player entity id (the 0x0A mark)
	name        string
	team        int
	mode        uint8
	deadlineMs  int64
	startX      float64
	startZ      float64
}

/*
================
flagWarArenaObject

One match-owned NPC-band object published as a create row and
intercepted on click.
================
*/
type flagWarArenaObject struct {
	gid       uint32
	kind      uint8
	poleIndex int
	side      int
	refObjID  uint32
	codename  string
	spawn     simulation.Spawn
	live      bool
}

/*
================
flagWarMatch

The live match in one division. One match per division: the event is
daily, so a second start in the same division is a cancel, not a queue.
================
*/
type flagWarMatch struct {
	division     string
	lease        instance.Lease
	startMs      int64
	endsMs       int64
	closeMs      int64 // 0 until the match ends
	participants map[int64]*flagWarParticipant
	order        []int64 // participant ids in entry order (frame fan-out is sorted, deterministic)

	poles    [3]bool // flying per pole index
	channels [3]*flagWarChannel

	carrierID      int64 // character id, 0 = nobody carries
	carrierSinceMs int64
	flagLive       bool // the dropped-flag NPC stands in the arena
	flagSpawn      simulation.Spawn

	objects map[uint32]*flagWarArenaObject

	keyMonsterGid [2]uint32
	keyGone       [2]bool
	keyTaken      [2]bool
	keyLastSpawn  [2]simulation.Spawn

	lastSyncMs int64
	announced  bool // the mission-start + create rows went out
	winner     int  // -1 undecided/draw, else the winning side
}

// flagWarMatches is the runtime's live-match book, keyed by division.
type flagWarMatches struct {
	byDivision map[string]*flagWarMatch
}

//============================================================================

/*
================
flagWarLevelOf

A participant's level: the character's About level, 0 when shapeless.
================
*/
func flagWarLevelOf(level *int64) int64 {
	if level == nil {
		return 0
	}
	return *level
}

/*
================
flagWarBracketFor

The match bracket from the mean participant level (PORT INFERENCE,
see the file header).
================
*/
func flagWarBracketFor(levels []int64) int {
	if len(levels) == 0 {
		return 0
	}
	var total int64
	for _, level := range levels {
		total += level
	}
	avg := total / int64(len(levels))
	bracket := (avg - 20) / 15
	if bracket < 0 {
		bracket = 0
	}
	if bracket > 6 {
		bracket = 6
	}
	return int(bracket)
}

/*
================
flagWarObjectGID
================
*/
func flagWarObjectGID(slot int) uint32 {
	return uint32(flagWarObjectGIDBase) + uint32(slot)
}

/*
================
flagWarPush

Push frames to one named character on the runtime's push lane (the
same lane the operator grant receipts ride). Nil-safe: unwired
runtimes (tests) simply do not push.
================
*/
func (rt *Runtime) flagWarPush(division, name string, frames ...simulation.Frame) {
	if rt.PushCharacterFrames == nil || len(frames) == 0 {
		return
	}
	wireFrames := make([]wire.Frame, len(frames))
	for i, frame := range frames {
		wireFrames[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload}
	}
	rt.PushCharacterFrames(division, name, wireFrames)
}

/*
================
flagWarMatchFor

The live match in a division, or nil.
================
*/
func (rt *Runtime) flagWarMatchFor(division string) *flagWarMatch {
	if rt.flagWarLive.byDivision == nil {
		return nil
	}
	return rt.flagWarLive.byDivision[division]
}

/*
================
flagWarParticipantFrames

One event frame to one participant.
================
*/
func flagWarParticipantFrames(p *flagWarParticipant, opcode uint16, payload []byte) simulation.DivisionFrames {
	return simulation.DivisionFrames{
		DivisionID:      p.registrant.Division,
		OnlyCharacterID: p.registrant.CharacterID,
		Frames:          []simulation.Frame{{Opcode: opcode, Payload: payload}},
	}
}

/*
================
flagWarAllFrames

One event frame to every entered participant, in entry order.
================
*/
func (m *flagWarMatch) allFrames(opcode uint16, payload []byte) []simulation.DivisionFrames {
	out := make([]simulation.DivisionFrames, 0, len(m.order))
	for _, id := range m.order {
		p := m.participants[id]
		if p == nil || !p.entered {
			continue
		}
		out = append(out, flagWarParticipantFrames(p, opcode, payload))
	}
	return out
}

/*
================
flagWarTeamFrames

One event frame to every entered participant of one side.
================
*/
func (m *flagWarMatch) teamFrames(team int, opcode uint16, payload []byte) []simulation.DivisionFrames {
	out := make([]simulation.DivisionFrames, 0, len(m.order))
	for _, id := range m.order {
		p := m.participants[id]
		if p == nil || !p.entered || p.team != team {
			continue
		}
		out = append(out, flagWarParticipantFrames(p, opcode, payload))
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

//============================================================================

/*
================
startFlagWarMatch

The scheduler's match hook (rt.FlagWarMatchStart). Registrants arrive
in sign-up order. Per division: a live match or a short house cancels
everyone with the native game-cancel notice; over the sixteen cap the
tail is turned away start-not-enter; the house splits into two teams
alternating by sign-up order (owner decision: even alternating split).

The native send order is game start, teleports, mission start: the
0x04 goes on the push lane BEFORE each transfer so it lands first,
and the mission start plus the arena create rows ride the match tick
that follows, after every relocation.
================
*/
func (rt *Runtime) startFlagWarMatch(registrants []FlagWarRegistrant) []simulation.DivisionFrames {
	nowMs := rt.Now().UnixMilli()
	byDivision := make(map[string][]FlagWarRegistrant)
	divisions := make([]string, 0, 4)
	for _, registrant := range registrants {
		if _, seen := byDivision[registrant.Division]; !seen {
			divisions = append(divisions, registrant.Division)
		}
		byDivision[registrant.Division] = append(byDivision[registrant.Division], registrant)
	}
	sort.Strings(divisions)

	var out []simulation.DivisionFrames
	for _, division := range divisions {
		out = append(out, rt.flagWarStartDivision(division, byDivision[division], nowMs)...)
	}
	return out
}

/*
================
flagWarCancelFrames

The game-cancel notice for every registrant of one division.
================
*/
func flagWarCancelFrames(registrants []FlagWarRegistrant) []simulation.DivisionFrames {
	out := make([]simulation.DivisionFrames, 0, len(registrants))
	for _, registrant := range registrants {
		out = append(out, simulation.DivisionFrames{
			DivisionID:      registrant.Division,
			OnlyCharacterID: registrant.CharacterID,
			Frames: []simulation.Frame{{
				Opcode:  wire.OpFlagWarEvent,
				Payload: wire.EncodeFlagWarEventSubtype(wire.FlagWarGameCancel),
			}},
		})
	}
	return out
}

/*
================
flagWarStartDivision

One division's match start: house rules, the instance open, the
transfers, the monsters, the live match state.
================
*/
func (rt *Runtime) flagWarStartDivision(division string, registrants []FlagWarRegistrant, nowMs int64) []simulation.DivisionFrames {
	settings := rt.flagWarSettings()
	if rt.flagWarMatchFor(division) != nil || len(registrants) < settings.MinPlayers {
		return flagWarCancelFrames(registrants)
	}
	accepted := registrants
	var turned []FlagWarRegistrant
	if len(accepted) > flagWarPlayerCap {
		turned = accepted[flagWarPlayerCap:]
		accepted = accepted[:flagWarPlayerCap]
	}
	var out []simulation.DivisionFrames
	for _, registrant := range turned {
		out = append(out, simulation.DivisionFrames{
			DivisionID:      registrant.Division,
			OnlyCharacterID: registrant.CharacterID,
			Frames: []simulation.Frame{{
				Opcode:  wire.OpFlagWarEvent,
				Payload: wire.EncodeFlagWarEventSubtype(wire.FlagWarStartNotEnter),
			}},
		})
	}

	if rt.Monsters == nil {
		// No monster state, no arena: cancel like a short house (this
		// is also how an unwired test runtime answers).
		return flagWarCancelFrames(accepted)
	}
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{
		Division:   division,
		Definition: instance.DefinitionID(29), // INS_EVENT_RANDOMMATCH, the flag war world
		DeadlineMs: nowMs + flagWarMatchDurationMs + flagWarDeadlineSlackMs,
	})
	if status != instance.Success {
		return append(out, flagWarCancelFrames(accepted)...)
	}

	match := &flagWarMatch{
		division:     division,
		lease:        lease,
		startMs:      nowMs,
		endsMs:       nowMs + flagWarMatchDurationMs,
		participants: make(map[int64]*flagWarParticipant),
		objects:      make(map[uint32]*flagWarArenaObject),
		winner:       -1,
	}
	match.poles[0] = true // the centre pole flies when the match starts

	levels := make([]int64, 0, len(accepted))
	for index, registrant := range accepted {
		participant := &flagWarParticipant{
			registrant: registrant,
			team:       index % 2, // even alternating split by sign-up order
		}
		unlock := rt.lockDivision(division)
		character := rt.findCharacter(division, registrant.Name)
		if character != nil {
			participant.level = flagWarLevelOf(character.Level)
		}
		unlock()
		if character == nil {
			continue // signed up, then left: no transfer, no seat
		}
		rt.flagWarPush(division, registrant.Name, simulation.Frame{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarEventType(wire.FlagWarGameStart),
		})
		if rt.TransferCharacterToInstance(division, character, lease, flagWarTeamSpawns[participant.team]) != instance.Success {
			continue
		}
		participant.entered = true
		match.participants[registrant.CharacterID] = participant
		match.order = append(match.order, registrant.CharacterID)
		levels = append(levels, participant.level)
	}
	if len(match.order) == 0 {
		// Nobody arrived: give the layer back and forget the match.
		rt.CloseInstanceWorld(division, lease)
		return out
	}

	rt.flagWarSpawnSides(match, levels, nowMs)
	rt.flagWarBuildObjects(match)

	if rt.flagWarLive.byDivision == nil {
		rt.flagWarLive.byDivision = make(map[string]*flagWarMatch)
	}
	rt.flagWarLive.byDivision[division] = match
	return out
}

/*
================
flagWarSpawnSides

Ten monsters per side around the side anchor; one per side carries
the key. PORT CHOICE: the scatter is a deterministic ring of radius
60..240 (native scatters randomly within 300), and the key carrier is
a fixed pick of the ten (native rolls rand%10). Dead side monsters do
not respawn (native evidence).
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

/*
================
flagWarReference

The RefObjID for an arena codename.
================
*/
func (rt *Runtime) flagWarReference(codename string) (uint32, bool) {
	if rt.Monsters == nil {
		return 0, false
	}
	ref, ok := rt.Monsters.ReferenceByCodename(codename)
	if !ok {
		return 0, false
	}
	return ref.RefObjID, true
}

//============================================================================

/*
================
advanceFlagWarMatches

The match tick step: mission start, reconciliation, channels, the
carrier clock, the key poll, the elapsed sync, the ending. Runs just
before the announcement drain (the step-order test pins the drain
last; PR 1 learned that once).
================
*/
func (rt *Runtime) advanceFlagWarMatches(nowMs int64) []simulation.DivisionFrames {
	if len(rt.flagWarLive.byDivision) == 0 {
		return nil
	}
	divisions := make([]string, 0, len(rt.flagWarLive.byDivision))
	for division := range rt.flagWarLive.byDivision {
		divisions = append(divisions, division)
	}
	sort.Strings(divisions)
	var out []simulation.DivisionFrames
	for _, division := range divisions {
		match := rt.flagWarLive.byDivision[division]
		if match == nil {
			continue
		}
		out = append(out, rt.flagWarTickMatch(match, nowMs)...)
		if match.closeMs != 0 && nowMs >= match.closeMs {
			rt.CloseInstanceWorld(match.division, match.lease)
			delete(rt.flagWarLive.byDivision, division)
		}
	}
	return out
}

/*
================
flagWarTickMatch

One match, one tick.
================
*/
func (rt *Runtime) flagWarTickMatch(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	if !match.announced {
		match.announced = true
		match.lastSyncMs = match.startMs
		for _, id := range match.order {
			p := match.participants[id]
			if p == nil || !p.entered {
				continue
			}
			out = append(out, flagWarParticipantFrames(p, wire.OpFlagWarEvent,
				wire.EncodeFlagWarMissionStart(uint16(p.team))))
			for _, object := range match.sortedObjects() {
				if !object.live {
					continue
				}
				out = append(out, flagWarParticipantFrames(p, wire.OpObjectListChunk, rt.flagWarObjectRow(object)))
			}
		}
	}
	out = append(out, rt.flagWarReconcile(match, nowMs)...)
	out = append(out, rt.flagWarTickChannels(match, nowMs)...)
	out = append(out, rt.flagWarTickCarrier(match, nowMs)...)
	out = append(out, rt.flagWarTickKeys(match, nowMs)...)
	if match.closeMs == 0 {
		if nowMs-match.lastSyncMs >= flagWarElapsedSyncMs && nowMs < match.endsMs {
			match.lastSyncMs = nowMs
			out = append(out, match.allFrames(wire.OpFlagWarEvent,
				wire.EncodeFlagWarElapsed(uint32(nowMs-match.startMs)))...)
		}
		if nowMs >= match.endsMs {
			out = append(out, rt.flagWarEndMatch(match, nowMs)...)
		}
	}
	return out
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
flagWarReconcile

Entered participants who left the instance world (disconnected,
rebirthed to town, walked out) leave the match: their channel dies,
a carried flag drops where they last stood, and their return anchor
is cleared so the closing world cannot pull them back from wherever
they now play.
================
*/
func (rt *Runtime) flagWarReconcile(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, id := range match.order {
		p := match.participants[id]
		if p == nil || !p.entered {
			continue
		}
		unlock := rt.lockDivision(match.division)
		character := rt.findCharacter(match.division, p.registrant.Name)
		gone := character == nil || instance.ID(domain.CharacterWorldInstance(character)) != match.lease.ID
		if character != nil && !gone {
			p.lastSpawn = rt.liveSpawn(simulation.WorldKey(match.division, character.Name), character, nowMs)
			p.hasPose = true
		}
		unlock()
		if character != nil && gone {
			rt.deps.Update(character, "flagwar-anchor-clear", func() bool {
				if character.World != nil {
					character.World.InstanceReturn = nil
				}
				return true
			})
		}
		if !gone {
			continue
		}
		p.entered = false
		for pole := 0; pole < 3; pole++ {
			if ch := match.channels[pole]; ch != nil && ch.characterID == id {
				match.channels[pole] = nil
			}
		}
		if match.carrierID == id {
			at := p.lastSpawn
			if !p.hasPose {
				at = flagWarTeamSpawns[p.team]
			}
			out = append(out, rt.flagWarDropFlag(match, at)...)
		}
	}
	return out
}

/*
================
flagWarTickChannels

Channel completion and movement cancellation. A channeler who moved
past the leeway, died, or left (reconciliation already cleared them)
loses the channel silently, the way walking breaks a native channel.
================
*/
func (rt *Runtime) flagWarTickChannels(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for pole := 0; pole < 3; pole++ {
		ch := match.channels[pole]
		if ch == nil {
			continue
		}
		channeler := match.participants[ch.characterID]
		if channeler == nil || !channeler.entered {
			match.channels[pole] = nil
			continue
		}
		if nowMs < ch.deadlineMs {
			if channeler.hasPose {
				dx := channeler.lastSpawn.X - ch.startX
				dz := channeler.lastSpawn.Z - ch.startZ
				if dx*dx+dz*dz > flagWarChannelMoveLeeway*flagWarChannelMoveLeeway {
					match.channels[pole] = nil
				}
			}
			continue
		}
		match.channels[pole] = nil
		out = append(out, rt.flagWarCompleteChannel(match, pole, ch, channeler, nowMs)...)
	}
	return out
}

/*
================
flagWarCompleteChannel

Steal: the pole comes down, the channeler carries, the taker's side
cheers and the victims hear the other notice. Score: the pole flies,
the mission result names the scorer, the carry ends.
================
*/
func (rt *Runtime) flagWarCompleteChannel(match *flagWarMatch, pole int, ch *flagWarChannel, channeler *flagWarParticipant, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	if ch.mode == 1 {
		match.poles[pole] = false
		out = append(out, rt.flagWarSwapPole(match, pole)...)
		match.carrierID = ch.characterID
		match.carrierSinceMs = nowMs
		out = append(out, match.allFrames(wire.OpFlagWarEvent,
			wire.EncodeFlagWarCarrier(ch.gid))...)
		out = append(out, match.teamFrames(ch.team, wire.OpFlagWarEvent,
			wire.EncodeFlagWarNamed(wire.FlagWarPickupOurs, ch.name))...)
		// The enemy notice carries no name natively (FlagWarClient.md).
		out = append(out, match.teamFrames(1-ch.team, wire.OpFlagWarEvent,
			wire.EncodeFlagWarEventSubtype(wire.FlagWarPickupTheirs))...)
		return out
	}
	match.poles[pole] = true
	out = append(out, rt.flagWarSwapPole(match, pole)...)
	match.carrierID = 0
	out = append(out, match.allFrames(wire.OpFlagWarEvent,
		wire.EncodeFlagWarMissionResult(uint8(ch.team), ch.name))...)
	return out
}

//============================================================================

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
flagWarDropFlag

The flag falls: the dropped-flag NPC stands at the spot and the drop
notice goes to the whole match.
================
*/
func (rt *Runtime) flagWarDropFlag(match *flagWarMatch, at simulation.Spawn) []simulation.DivisionFrames {
	match.carrierID = 0
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
flagWarEndMatch

The clock ends. The side whose team pole flies wins (both or neither
flying is a draw - PORT CHOICE where the native is silent): winners
take the win notice and one trophy each, everyone's timer stops, and
the world closes a minute later onto the return anchors.
================
*/
func (rt *Runtime) flagWarEndMatch(match *flagWarMatch, nowMs int64) []simulation.DivisionFrames {
	match.closeMs = nowMs + flagWarCloseAfterEndMs
	side0, side1 := match.poles[1], match.poles[2]
	match.winner = -1
	if side0 != side1 {
		if side0 {
			match.winner = 0
		} else {
			match.winner = 1
		}
	}
	out := match.allFrames(wire.OpFlagWarEvent, wire.EncodeFlagWarEventType(wire.FlagWarTimerStop))
	if match.winner >= 0 {
		out = append(out, match.teamFrames(match.winner, wire.OpFlagWarEvent,
			wire.EncodeFlagWarEventSubtype(wire.FlagWarGameWin))...)
		for _, id := range match.order {
			p := match.participants[id]
			if p == nil || !p.entered || p.team != match.winner {
				continue
			}
			rt.flagWarGrantTrophy(match.division, p.registrant.Name)
		}
	}
	return out
}

/*
================
flagWarGrantTrophy

One trophy, through the operator grant lane (the quest-shaped mint:
refuses mid-bargain, pushes the ordinary item receipts). A failed
mint is retried never and logged by the caller surface - the match
test pins the exact mint counts on the success path.
================
*/
func (rt *Runtime) flagWarGrantTrophy(division, name string) {
	_ = rt.OperatorGrantItems(division, name, []inventory.ItemAmount{
		{Codename: flagWarTrophyCodename, Count: 1},
	})
}

//============================================================================

/*
================
flagWarObjectFor

The match object a gid names in this division, or nil. The select
handler consults this BEFORE roster resolution: these gids exist on
no roster, so nothing else can shadow them in practice.
================
*/
func (rt *Runtime) flagWarObjectFor(division string, gid uint32) (*flagWarMatch, *flagWarArenaObject) {
	match := rt.flagWarMatchFor(division)
	if match == nil {
		return nil, nil
	}
	object := match.objects[gid]
	if object == nil || !object.live {
		return nil, nil
	}
	return match, object
}

/*
================
flagWarSelect

A click on a match object. The answer is the ordinary NPC grant
(len-11 shape, no capabilities, no event byte: these objects open no
talk); the match rules run on top. Beyond the interaction range the
answer is the native too-far refusal, exactly like a roster NPC -
the client walks in and the next click lands.
================
*/
func (rt *Runtime) flagWarSelect(divisionID string, character *enterworld.Character, gid uint32) (SelectOutcome, bool) {
	match, object := rt.flagWarObjectFor(divisionID, gid)
	if match == nil {
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
	object.live = false
	match.flagLive = false
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
	match.keyTaken[object.side] = true
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

//============================================================================

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
	diff := flagWarLevelOf(killer.Level) - dead.level
	if diff < 0 {
		diff = -diff
	}
	if diff <= flagWarKeyLevelWindow {
		rt.flagWarGrantTrophy(division, hunter.registrant.Name)
	}
}
