/*
===========================================================================

flagwar.go - Capture the Flag: registration and the daily schedule

Native ownership is split: the ShardManager keeps the registration
queue and the clock, each GameServer keeps a mirror list and answers
its own players. This port runs one GameWorld for the shard, so the
action runtime owns the queue, the clock and the answers in one place.

The registration host is the town event NPC NPC_CH_EVENT_KISAENG1
(the "Event So-Ok" of the standby texts): the 0xB45A select answer
carries the event byte only for it and only while the event is active
(the reference server's event writer) - 1 offers the join
row, 2 the cancel row for a character already in the queue. The talk
row's click sends the empty 0x7753, which toggles registration; the
answer is the [0xFF, 0x00] join-result notice (the client picks the
success/cancel text from its own row state), [0xFF, 0x05] below level
20 and [0xFF, 0x15] when the event cannot be entered.

The schedule is one match a day at a fixed UTC time (owner,
2026-10-24): sign-up opens ten minutes before the start and the
standby notices fire at -10/-5/-1 minutes to every online player.
SRO_FLAGWAR_START_UTC moves the start (default 20:00) and
SRO_FLAGWAR_MIN_PLAYERS the house minimum (default 4); a start with
fewer registrants cancels with the native game-cancel notice, whose
text is exactly that the required number was not met. With enough
registrants the match owner takes the queue in sign-up order through
FlagWarMatchStart; until that lane lands, a full house cancels like
an empty one.

===========================================================================
*/

package action

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvFlagWarStartUTC moves the daily Capture the Flag start ("HH:MM", UTC).
const EnvFlagWarStartUTC = "SRO_FLAGWAR_START_UTC"

// EnvFlagWarMinPlayers sets the smallest house that starts a match.
const EnvFlagWarMinPlayers = "SRO_FLAGWAR_MIN_PLAYERS"

const (
	// flagWarDefaultStartMinute is 20:00 UTC in minutes of day.
	flagWarDefaultStartMinute = 20 * 60
	// flagWarDefaultMinPlayers is the owner's default house minimum.
	flagWarDefaultMinPlayers = 4
	// flagWarMinPlayersFloor: a team split needs at least two.
	flagWarMinPlayersFloor = 2
	// flagWarMinPlayersCeiling is world 29's catalog PlayerLimit (16):
	// a larger minimum could never start a match.
	flagWarMinPlayersCeiling = 16
	// flagWarSignupMinutes is the native sign-up window before a start.
	flagWarSignupMinutes = 10
	// flagWarLevelMinimum is the native registration gate (the reference server's register check).
	flagWarLevelMinimum = 20
	// flagWarRegistrationNPC is the codename the select byte is gated
	// on (a substring test natively).
	flagWarRegistrationNPC = "NPC_CH_EVENT_KISAENG1"
)

/*
================
FlagWarSettings

The Capture the Flag schedule: the daily start as a UTC minute of day
and the smallest registered house that starts a match. The zero value
is the native default (20:00 UTC, 4 players).
================
*/
type FlagWarSettings struct {
	StartMinuteOfDay int
	MinPlayers       int
}

/*
================
DefaultFlagWarSettings

DefaultFlagWarSettings is the schedule with no environment override.
================
*/
func DefaultFlagWarSettings() FlagWarSettings {
	return FlagWarSettings{StartMinuteOfDay: flagWarDefaultStartMinute, MinPlayers: flagWarDefaultMinPlayers}
}

/*
================
FlagWarSettingsFromEnv

Composition reads this once. An invalid value refuses startup instead
of silently moving a daily event.
================
*/
func FlagWarSettingsFromEnv() (FlagWarSettings, error) {
	settings := DefaultFlagWarSettings()
	if value := strings.TrimSpace(os.Getenv(EnvFlagWarStartUTC)); value != "" {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return FlagWarSettings{}, fmt.Errorf("%s must be HH:MM in UTC, got %q", EnvFlagWarStartUTC, value)
		}
		hour, hourErr := strconv.Atoi(parts[0])
		minute, minuteErr := strconv.Atoi(parts[1])
		if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return FlagWarSettings{}, fmt.Errorf("%s must be HH:MM in UTC, got %q", EnvFlagWarStartUTC, value)
		}
		settings.StartMinuteOfDay = hour*60 + minute
	}
	if value := strings.TrimSpace(os.Getenv(EnvFlagWarMinPlayers)); value != "" {
		minimum, err := strconv.Atoi(value)
		if err != nil || minimum < flagWarMinPlayersFloor || minimum > flagWarMinPlayersCeiling {
			return FlagWarSettings{}, fmt.Errorf("%s must be a player count between %d and %d, got %q",
				EnvFlagWarMinPlayers, flagWarMinPlayersFloor, flagWarMinPlayersCeiling, value)
		}
		settings.MinPlayers = minimum
	}
	return settings, nil
}

/*
================
normalized

A zero settings field (an unwired runtime) still schedules the native
default rather than a midnight four-o-clock accident.
================
*/
func (s FlagWarSettings) normalized() FlagWarSettings {
	if s == (FlagWarSettings{}) {
		return DefaultFlagWarSettings()
	}
	return s
}

/*
================
startOn

The start instant of the UTC day that contains now.
================
*/
func (s FlagWarSettings) startOn(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(),
		s.StartMinuteOfDay/60, s.StartMinuteOfDay%60, 0, 0, time.UTC)
}

/*
================
signupOpen

Sign-up is open in the ten minutes before today's start.
================
*/
func (s FlagWarSettings) signupOpen(now time.Time) bool {
	start := s.startOn(now)
	return !now.Before(start.Add(-flagWarSignupMinutes*time.Minute)) && now.Before(start)
}

/*
================
FlagWarRegistrant

One queued registration, in sign-up order. The match owner consumes
these at the start instant: Division and CharacterID route its
teleports and notices, Gid names the player entity.
================
*/
type FlagWarRegistrant struct {
	Division    string
	CharacterID int64
	Name        string
	Gid         uint32
	Seq         int64
}

/*
================
flagWarEvent

The registration queue and the scheduler's last tick. Process-local,
like the native GameServer mirror list: a restart forgets the queue,
which a daily event tolerates (sign-up reopens every day).
================
*/
type flagWarEvent struct {
	mu          sync.Mutex
	registrants map[string]FlagWarRegistrant
	seq         int64
	lastTickMs  int64
	ticked      bool
}

/*
================
flagWarKey
================
*/
func flagWarKey(divisionID, characterName string) string {
	return divisionID + "\x00" + strings.ToLower(characterName)
}

/*
================
flagWarSettings

The runtime's schedule, defaulted when unwired.
================
*/
func (rt *Runtime) flagWarSettings() FlagWarSettings {
	return rt.FlagWar.normalized()
}

/*
================
HandleFlagWarRegister

HandleFlagWarRegister answers the empty C->S 0x7753: it toggles the
bound character's registration while sign-up is open. Below the
native level gate the answer is [0xFF, 0x05]; outside the window, or
for a character already inside an instanced world, [0xFF, 0x15]; a
toggle answers [0xFF, 0x00].
================
*/
func (rt *Runtime) HandleFlagWarRegister(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	if character == nil {
		return OpResult{DiagnosticRefusal: "characterNotFound"}
	}
	if len(payload) != 0 {
		return OpResult{DiagnosticRefusal: "flag war register body must be empty"}
	}
	answer := func(subtype uint8) OpResult {
		return OpResult{Frames: []wire.Frame{{
			Opcode:  wire.OpFlagWarEvent,
			Payload: wire.EncodeFlagWarEventSubtype(subtype),
		}}}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	level := int64(1)
	deletePending, instanced := false, false
	rt.deps.Read(divisionID, func() {
		deletePending = character.DeletePending
		if character.Level != nil {
			level = *character.Level
		}
		instanced = domain.CharacterWorldInstance(character) != domain.DefaultWorldInstance
	})
	if deletePending {
		return OpResult{DiagnosticRefusal: "deletePending"}
	}
	if !rt.flagWarSettings().signupOpen(rt.Now()) {
		return answer(wire.FlagWarEnteredError)
	}
	if level < flagWarLevelMinimum {
		return answer(wire.FlagWarLevelError)
	}
	if instanced {
		return answer(wire.FlagWarEnteredError)
	}
	rt.flagWarToggle(divisionID, character)
	return answer(wire.FlagWarJoinResult)
}

/*
================
flagWarToggle

Joins the queue, or leaves it when already queued. Returns the new
registration state.
================
*/
func (rt *Runtime) flagWarToggle(divisionID string, character *enterworld.Character) bool {
	event := &rt.flagWar
	event.mu.Lock()
	defer event.mu.Unlock()
	if event.registrants == nil {
		event.registrants = make(map[string]FlagWarRegistrant)
	}
	key := flagWarKey(divisionID, character.Name)
	if _, queued := event.registrants[key]; queued {
		delete(event.registrants, key)
		return false
	}
	event.seq++
	event.registrants[key] = FlagWarRegistrant{
		Division:    divisionID,
		CharacterID: character.ID,
		Name:        character.Name,
		Gid:         enterworld.ObjectIDForCharacter(character),
		Seq:         event.seq,
	}
	return true
}

/*
================
flagWarRegistered
================
*/
func (rt *Runtime) flagWarRegistered(divisionID, characterName string) bool {
	event := &rt.flagWar
	event.mu.Lock()
	defer event.mu.Unlock()
	_, queued := event.registrants[flagWarKey(divisionID, characterName)]
	return queued
}

/*
================
flagWarSelectByte

The 0xB45A event byte for one NPC select answer (the reference server's event writer): zero for
every NPC but the registration host, and for it zero while sign-up is
closed; 1 invites an unregistered character, 2 offers the cancel.
================
*/
func (rt *Runtime) flagWarSelectByte(divisionID string, character *enterworld.Character, npc simulation.NpcDef) uint8 {
	if character == nil || !strings.Contains(npc.Codename, flagWarRegistrationNPC) {
		return 0
	}
	if !rt.flagWarSettings().signupOpen(rt.Now()) {
		return 0
	}
	if rt.flagWarRegistered(divisionID, character.Name) {
		return 2
	}
	return 1
}

/*
================
advanceFlagWar

The scheduler's tick step: the standby notices broadcast to every
online player as their instants pass, and the start instant resolves
the queue. Edges fire on the first tick at or after them, in order,
so a stalled tick announces late rather than never.
================
*/
func (rt *Runtime) advanceFlagWar(nowMs int64) []simulation.DivisionFrames {
	settings := rt.flagWarSettings()
	start := settings.startOn(time.UnixMilli(nowMs))

	event := &rt.flagWar
	event.mu.Lock()
	if !event.ticked {
		event.ticked = true
		event.lastTickMs = nowMs
	}
	previous := event.lastTickMs
	event.lastTickMs = nowMs
	event.mu.Unlock()

	edges := []struct {
		at   time.Time
		fire func() []simulation.DivisionFrames
	}{
		{start.Add(-flagWarSignupMinutes * time.Minute),
			func() []simulation.DivisionFrames { return rt.flagWarBroadcast(wire.FlagWarStandby10) }},
		{start.Add(-5 * time.Minute),
			func() []simulation.DivisionFrames { return rt.flagWarBroadcast(wire.FlagWarStandby5) }},
		{start.Add(-time.Minute),
			func() []simulation.DivisionFrames { return rt.flagWarBroadcast(wire.FlagWarStandby1) }},
		{start, func() []simulation.DivisionFrames { return rt.flagWarStart(settings) }},
	}
	var out []simulation.DivisionFrames
	for _, edge := range edges {
		if previous < edge.at.UnixMilli() && edge.at.UnixMilli() <= nowMs {
			out = append(out, edge.fire()...)
		}
	}
	return out
}

/*
================
flagWarBroadcast

One event notice to every division with admitted players.
================
*/
func (rt *Runtime) flagWarBroadcast(eventType uint8) []simulation.DivisionFrames {
	divisions := map[string]bool{}
	rt.characterAdmissions.Range(func(_, value any) bool {
		divisions[value.(populationAdmission).division] = true
		return true
	})
	names := make([]string, 0, len(divisions))
	for division := range divisions {
		names = append(names, division)
	}
	sort.Strings(names)
	out := make([]simulation.DivisionFrames, 0, len(names))
	for _, division := range names {
		out = append(out, simulation.DivisionFrames{
			DivisionID: division,
			Frames: []simulation.Frame{{
				Opcode:  wire.OpFlagWarEvent,
				Payload: wire.EncodeFlagWarEventType(eventType),
			}},
		})
	}
	return out
}

/*
================
flagWarStart

The start instant: the queue drains in sign-up order. The match
owner, when installed, takes a full house from there; anything else
cancels every registrant with the native game-cancel notice and the
queue clears for tomorrow.
================
*/
func (rt *Runtime) flagWarStart(settings FlagWarSettings) []simulation.DivisionFrames {
	event := &rt.flagWar
	event.mu.Lock()
	registrants := make([]FlagWarRegistrant, 0, len(event.registrants))
	for _, registrant := range event.registrants {
		registrants = append(registrants, registrant)
	}
	event.registrants = make(map[string]FlagWarRegistrant)
	event.mu.Unlock()
	sort.Slice(registrants, func(i, j int) bool { return registrants[i].Seq < registrants[j].Seq })

	if rt.FlagWarMatchStart != nil && len(registrants) >= settings.MinPlayers {
		return rt.FlagWarMatchStart(registrants)
	}
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
