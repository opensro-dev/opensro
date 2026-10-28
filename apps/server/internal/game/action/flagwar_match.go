/*
===========================================================================

flagwar_match.go - the Capture the Flag match owner

PR 2 of the flag war (#887): the daily match itself. PR 1 shipped the
registration scheduler; this file is the rt.FlagWarMatchStart hook the
scheduler fires at the start instant, the match tick (channels, the
carrier, the elapsed sync, the ending), and the reward mint queue.
The arena objects, the click/combat interactions and the side
monsters live in flagwar_arena.go.

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
  - The monster family follows the match level bracket byte 0..6.
    The reference server fields seven level-tagged event monsters;
    the shipped v1.150 characterdata carries the same event family
    under its own names and levels: MOB_EV_WOLF (level 25, brackets
    0-1), MOB_EV_RAVEN (45, bracket 2), MOB_EV_PENGUIN (60, bracket
    3), MOB_EV_KANGAROO (75, bracket 4), MOB_EV_JINN (85, bracket 5).
    No 95-class event monster ships in v1.150 data, so bracket 6
    fields the jinn again (PORT CHOICE, recorded here).
    PORT INFERENCE (recorded, not a native fact): the port flies one
    bracket per match from the mean participant level,
    clamp((avg-20)/15, 0, 6).
  - Channels run five seconds; the carrier drops the flag after sixty
    seconds (0xEA60 = 60000 ms); the elapsed sync rides every ten
    seconds; the match clock is twenty minutes (client timer
    0x124F80 ms). The winner is the side whose team pole flies when
    the clock ends; the centre pole alone never wins.
  - Rewards: every winning-team player takes one
    ITEM_ETC_E070919_TROPHY (owner substitution for the reference
    server's ice trophy, which does not exist in the shipped data);
    the flag carrier's killer takes one when the two levels are
    within nine.

Port choices (labelled, reversible): teams split evenly alternating by
sign-up order; a match object (pole, dropped flag, master key, door)
is a match-owned NPC-band object whose clicks are intercepted before
roster resolution, because a click must arrive as an NPC interaction,
not a monster attack; the arena monsters scatter
on a deterministic ring instead of the native random scatter; dead
side monsters do not respawn.

===========================================================================
*/

package action

import (
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"

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

// The monster codename per level bracket 0..6. These are the event
// monsters that ship in v1.150 characterdata (see the file header);
// the reference server's level-tagged names do not exist in the
// shipped data, and bracket 6 reuses the jinn because no 95-class
// event monster ships.
var flagWarBracketCodenames = [7]string{
	"MOB_EV_WOLF",     // 0 (level 25)
	"MOB_EV_WOLF",     // 1 (level 25)
	"MOB_EV_RAVEN",    // 2 (level 45)
	"MOB_EV_PENGUIN",  // 3 (level 60)
	"MOB_EV_KANGAROO", // 4 (level 75)
	"MOB_EV_JINN",     // 5 (level 85)
	"MOB_EV_JINN",     // 6 (level 85; no 95-class ships)
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
	// mu guards every field below: the tick, the request lanes
	// (select, attack relations) and the death hook all reach the
	// match book, on different locks of their own. Lock order is the
	// division lane first, then mu (the request lanes already hold
	// the lane when they arrive); mu is never held across a call
	// that takes the division lane (transfers, world closes, trophy
	// mints all run after it is released).
	mu           sync.Mutex
	division     string
	lease        instance.Lease
	startMs      int64
	endsMs       int64
	closeMs      int64 // 0 until the match ends
	participants map[int64]*flagWarParticipant
	order        []int64 // participant ids in entry order (frame fan-out is sorted, deterministic)

	// Trophy mints owed, by character name. The killer's mint is
	// decided on the death lane, which already holds the division
	// lock the mint lane takes, so the mint itself is queued here
	// and drained by the match tick outside every lane.
	pendingMints []string

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

/*
================
flagWarMatches

The runtime's live-match book, keyed by division. mu guards the map
only (a leaf lock: no other lock is taken under it); each match then
guards its own book with its own mu.
================
*/
type flagWarMatches struct {
	mu         sync.Mutex
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
	rt.flagWarLive.mu.Lock()
	defer rt.flagWarLive.mu.Unlock()
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

	// Publish last: until this store no other lane can see the match,
	// so the build above needs no lock of its own.
	rt.flagWarLive.mu.Lock()
	if rt.flagWarLive.byDivision == nil {
		rt.flagWarLive.byDivision = make(map[string]*flagWarMatch)
	}
	rt.flagWarLive.byDivision[division] = match
	rt.flagWarLive.mu.Unlock()
	return out
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
	rt.flagWarLive.mu.Lock()
	divisions := make([]string, 0, len(rt.flagWarLive.byDivision))
	for division := range rt.flagWarLive.byDivision {
		divisions = append(divisions, division)
	}
	sort.Strings(divisions)
	matches := make([]*flagWarMatch, 0, len(divisions))
	for _, division := range divisions {
		if match := rt.flagWarLive.byDivision[division]; match != nil {
			matches = append(matches, match)
		}
	}
	rt.flagWarLive.mu.Unlock()

	var out []simulation.DivisionFrames
	for _, match := range matches {
		out = append(out, rt.flagWarTickMatch(match, nowMs)...)

		// Mints and the world close take the division lane: they run
		// here, after the match lock is released, so a request lane
		// holding the division lock and waiting on the match can
		// never meet this tick waiting back.
		match.mu.Lock()
		mints := match.pendingMints
		match.pendingMints = nil
		closeNow := match.closeMs != 0 && nowMs >= match.closeMs
		match.mu.Unlock()
		for _, name := range mints {
			rt.flagWarGrantTrophy(match.division, name)
		}
		if closeNow {
			rt.CloseInstanceWorld(match.division, match.lease)
			// A fallible evacuation leaves the layer retiring. Keep its match
			// policy until the instance owner has actually released that lease.
			if live, open := rt.Monsters.PopulationLease(match.division, match.lease.ID); open && live == match.lease {
				continue
			}
			rt.flagWarLive.mu.Lock()
			if rt.flagWarLive.byDivision[match.division] == match {
				delete(rt.flagWarLive.byDivision, match.division)
			}
			rt.flagWarLive.mu.Unlock()
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
	match.mu.Lock()
	if !match.announced {
		match.announced = true
		match.lastSyncMs = match.startMs
		for _, id := range match.order {
			p := match.participants[id]
			if p == nil || !p.entered {
				continue
			}
			for _, frame := range rt.flagWarSceneFrames(match, p, nowMs) {
				out = append(out, flagWarParticipantFrames(p, frame.Opcode, frame.Payload))
			}
		}
	}
	over := match.closeMs != 0
	match.mu.Unlock()
	if over {
		// The clock ended: the play lanes stay quiet until the close.
		return out
	}

	// Reconciliation takes the division lane in short bursts, so it
	// runs between the two locked halves, never under the match lock.
	out = append(out, rt.flagWarReconcile(match, nowMs)...)

	match.mu.Lock()
	out = append(out, rt.flagWarTickChannels(match, nowMs)...)
	out = append(out, rt.flagWarTickCarrier(match, nowMs)...)
	out = append(out, rt.flagWarTickKeys(match, nowMs)...)
	if nowMs-match.lastSyncMs >= flagWarElapsedSyncMs && nowMs < match.endsMs {
		match.lastSyncMs = nowMs
		out = append(out, match.allFrames(wire.OpFlagWarEvent,
			wire.EncodeFlagWarElapsed(uint32(nowMs-match.startMs)))...)
	}
	if nowMs >= match.endsMs {
		out = append(out, rt.flagWarEndMatch(match, nowMs)...)
	}
	match.mu.Unlock()
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
	// Phase 1 reads world truth under the division lane, exactly as
	// before - but with the match lock dropped (lock order: lane,
	// then match). Phase 2 applies the departures to the book.
	type sighting struct {
		p         *flagWarParticipant
		character *enterworld.Character
		gone      bool
		pose      simulation.Spawn
	}
	match.mu.Lock()
	entered := make([]*flagWarParticipant, 0, len(match.order))
	for _, id := range match.order {
		if p := match.participants[id]; p != nil && p.entered {
			entered = append(entered, p)
		}
	}
	match.mu.Unlock()

	sightings := make([]sighting, 0, len(entered))
	for _, p := range entered {
		s := sighting{p: p, gone: true}
		unlock := rt.lockDivision(match.division)
		s.character = rt.findCharacter(match.division, p.registrant.Name)
		if s.character != nil &&
			instance.ID(domain.CharacterWorldInstance(s.character)) == match.lease.ID {
			s.gone = false
			s.pose = rt.liveSpawn(simulation.WorldKey(match.division, s.character.Name), s.character, nowMs)
		}
		unlock()
		if s.character != nil && s.gone {
			character := s.character
			rt.deps.UpdateDeferred(character, "flagwar-anchor-clear", func() bool {
				if character.World != nil {
					character.World.InstanceReturn = nil
				}
				return true
			})
		}
		sightings = append(sightings, s)
	}

	var out []simulation.DivisionFrames
	match.mu.Lock()
	defer match.mu.Unlock()
	for _, s := range sightings {
		p := s.p
		if !p.entered {
			continue // another lane settled this departure first
		}
		if !s.gone {
			p.lastSpawn = s.pose
			p.hasPose = true
			continue
		}
		p.entered = false
		for pole := 0; pole < 3; pole++ {
			if ch := match.channels[pole]; ch != nil && ch.characterID == p.registrant.CharacterID {
				match.channels[pole] = nil
			}
		}
		if match.carrierID == p.registrant.CharacterID {
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
		// Single-flag rule: while a carrier carries or the dropped
		// flag stands, a completing steal only grounds the pole -
		// nobody becomes a second carrier (the client tracks one
		// carrier gid; two carriers desync it).
		if match.carrierID != 0 || match.flagLive {
			return out
		}
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
	// Score: the channeler must still be the carrier. A fall during
	// the channel resets the channel set, so this is the second lock
	// on the same door - never score a flag nobody carries home.
	if match.carrierID != ch.characterID {
		return out
	}
	match.poles[pole] = true
	out = append(out, rt.flagWarSwapPole(match, pole)...)
	match.carrierID = 0
	out = append(out, match.allFrames(wire.OpFlagWarEvent,
		wire.EncodeFlagWarMissionResult(uint8(ch.team), ch.name))...)
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
			match.pendingMints = append(match.pendingMints, p.registrant.Name)
		}
	}
	return out
}

/*
================
flagWarGrantTrophy

One trophy, through the operator grant lane (the quest-shaped mint:
refuses mid-bargain, pushes the ordinary item receipts). Called only
from the match tick's drain, with no lane held: the grant lane takes
the division lock itself, which is exactly why the death hook queues
its mint instead of granting inline. A failed mint is logged and not
retried - the match test pins the exact mint counts on the success
path.
================
*/
func (rt *Runtime) flagWarGrantTrophy(division, name string) {
	if err := rt.OperatorGrantItems(division, name, []inventory.ItemAmount{
		{Codename: flagWarTrophyCodename, Count: 1},
	}); err != nil {
		log.WithError(err).WithField("character", name).Warn("flagwar: trophy mint failed")
	}
}
