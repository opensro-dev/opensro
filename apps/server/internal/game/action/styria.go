/*
===========================================================================

styria.go - Styria Clash: schedule, registration and the arena manager

Port-only, not native (epic #3648, this is its first server slice).
The later client's job arena pits Hunters against Thieves in world
107; the later server runs it on its Battle Arena engine, whose shape
this file follows where the sources show it:

  - Two schedule windows per arena type (the later server's
    SCHEDULE_DEF_BATTLE_ARENA_JOB_PARTICIPATION, then
    SCHEDULE_DEF_BATTLE_ARENA_JOB): applications open in a
    participation window, matches start when it closes. The clock
    times themselves sit in that server's schedule rows, which no
    source here carries, so the port runs one daily UTC start with
    the flag-war sign-up length ahead of it (PORT CHOICE, below).
  - Applying is the arena manager's business (NPC_BATTLE_ARENA_MANAGER,
    service 0x21): the manager's own text admits only a character
    wearing a job suit, and a party forms only after entering. The
    port adds one rule the text implies: one live application per
    character (its already-applied answer offers the cancel).
  - Sides are the worn suit's job: a thief suit fights for the
    Thieves; a trader or hunter suit for the Hunters (PORT INFERENCE:
    the manager names the sides Hunters and Thieves, and in v1.150's
    job war the trader and the hunter stand on the same side).

With SRO_STYRIA_CLASH off, none of this exists: no manager stands in
the roster, no talk row is offered, no world is registered and this
file's tick is a nil check. The match itself lives in
styria_match.go, its treasure and scoring in styria_score.go;
rewards are a later slice.

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
	"opensro.online/server/internal/game/world/styria"
)

// EnvStyriaStartUTC moves the daily Styria Clash start ("HH:MM", UTC).
const EnvStyriaStartUTC = "SRO_STYRIA_CLASH_START_UTC"

// EnvStyriaMinPerSide sets the smallest side that starts a match.
const EnvStyriaMinPerSide = "SRO_STYRIA_CLASH_MIN_PER_SIDE"

const (
	// styriaDefaultStartMinute is 20:00 UTC in minutes of day (PORT
	// CHOICE: the later server's own start sits in its schedule
	// rows; the port's daily UTC shape follows Capture the Flag).
	styriaDefaultStartMinute = 20 * 60
	// styriaDefaultMinPerSide: the epic's done-when fields one
	// Hunter against one Thief, so one is the floor.
	styriaDefaultMinPerSide = 1
	// styriaMinPerSideCeiling keeps a configured minimum startable:
	// a side fields at most styriaTeamCap entrants.
	styriaMinPerSideCeiling = 16
	// styriaParticipationMinutes is the application window ahead of
	// the start (PORT CHOICE: the native flag-war sign-up length;
	// the later server's own window sits in its schedule rows).
	styriaParticipationMinutes = 10
)

// The two sides. A participant's side is the worn job suit's side.
const (
	styriaSideHunters = 0
	styriaSideThieves = 1
)

/*
================
StyriaSettings

The Styria Clash schedule: the daily start as a UTC minute of day
and the smallest side that starts a match. The zero value is the
port default (20:00 UTC, one per side).
================
*/
type StyriaSettings struct {
	StartMinuteOfDay int
	MinPerSide       int
}

/*
================
DefaultStyriaSettings

DefaultStyriaSettings is the schedule with no environment override.
================
*/
func DefaultStyriaSettings() StyriaSettings {
	return StyriaSettings{StartMinuteOfDay: styriaDefaultStartMinute, MinPerSide: styriaDefaultMinPerSide}
}

/*
================
StyriaSettingsFromEnv

Composition reads this once. An invalid value refuses startup
instead of silently moving a daily event.
================
*/
func StyriaSettingsFromEnv() (StyriaSettings, error) {
	settings := DefaultStyriaSettings()
	if value := strings.TrimSpace(os.Getenv(EnvStyriaStartUTC)); value != "" {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return StyriaSettings{}, fmt.Errorf("%s must be HH:MM in UTC, got %q", EnvStyriaStartUTC, value)
		}
		hour, hourErr := strconv.Atoi(parts[0])
		minute, minuteErr := strconv.Atoi(parts[1])
		if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return StyriaSettings{}, fmt.Errorf("%s must be HH:MM in UTC, got %q", EnvStyriaStartUTC, value)
		}
		settings.StartMinuteOfDay = hour*60 + minute
	}
	if value := strings.TrimSpace(os.Getenv(EnvStyriaMinPerSide)); value != "" {
		minimum, err := strconv.Atoi(value)
		if err != nil || minimum < 1 || minimum > styriaMinPerSideCeiling {
			return StyriaSettings{}, fmt.Errorf("%s must be a player count between %d and %d, got %q",
				EnvStyriaMinPerSide, 1, styriaMinPerSideCeiling, value)
		}
		settings.MinPerSide = minimum
	}
	return settings, nil
}

/*
================
normalized

A zero settings field (an unwired runtime) still schedules the
port default rather than a midnight accident.
================
*/
func (s StyriaSettings) normalized() StyriaSettings {
	if s == (StyriaSettings{}) {
		return DefaultStyriaSettings()
	}
	return s
}

/*
================
startOn

The start instant of the UTC day that contains now.
================
*/
func (s StyriaSettings) startOn(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(),
		s.StartMinuteOfDay/60, s.StartMinuteOfDay%60, 0, 0, time.UTC)
}

/*
================
nearStarts

Today's start and tomorrow's. A start in the first minutes after
midnight UTC has its participation window on the day before, so
every check looks at both; the two are a day apart, so at most one
of them is ever in reach.
================
*/
func (s StyriaSettings) nearStarts(now time.Time) [2]time.Time {
	today := s.startOn(now)
	return [2]time.Time{today, today.AddDate(0, 0, 1)}
}

/*
================
participationOpen

Applications are open in the window before a start, even when those
minutes fall on the day before it (a start before 00:10 UTC).
================
*/
func (s StyriaSettings) participationOpen(now time.Time) bool {
	for _, start := range s.nearStarts(now) {
		if !now.Before(start.Add(-styriaParticipationMinutes*time.Minute)) && now.Before(start) {
			return true
		}
	}
	return false
}

/*
================
styriaSettings

The runtime's schedule, defaulted when unwired.
================
*/
func (rt *Runtime) styriaSettings() StyriaSettings {
	return rt.Styria.normalized()
}

/*
================
styriaSideOf

The arena side a worn job suit fights for: a thief suit for the
Thieves, a trader or hunter suit for the Hunters (see the file
header). No suit reads -1.
================
*/
func styriaSideOf(job uint8) int {
	switch job {
	case domain.JobThief:
		return styriaSideThieves
	case domain.JobTrader, domain.JobHunter:
		return styriaSideHunters
	}
	return -1
}

/*
================
styriaSideName
================
*/
func styriaSideName(side int) string {
	if side == styriaSideThieves {
		return "Thief"
	}
	return "Hunter"
}

/*
================
StyriaRegistrant

One queued application, in sign-up order. The match owner consumes
these at the start instant: Division and CharacterID route its
teleports, Gid names the player entity, Side the entry gate.
================
*/
type StyriaRegistrant struct {
	Division    string
	CharacterID int64
	Name        string
	Gid         uint32
	Side        int
	Seq         int64
}

/*
================
styriaEvent

The registration book, the scheduler's last tick and the live match
book (styria_match.go), keyed by division. Process-local: a restart
forgets the queue, which a daily event tolerates (participation
reopens every day). Built only while the switch is on
(ConfigureStyria); a nil event is the whole feature off.
================
*/
type styriaEvent struct {
	mu          sync.Mutex
	registrants map[string]StyriaRegistrant
	seq         int64
	lastTickMs  int64
	ticked      bool
	matches     map[string]*styriaMatch

	// matchSeq counts the matches this process has started; a
	// status snapshot names its match by it, so the client can
	// hold one result until dismissed. statusSent is the last
	// snapshot bytes pushed per viewer (styria_status.go).
	matchSeq   uint32
	statusSent map[string][]byte
}

/*
================
styriaKey
================
*/
func styriaKey(divisionID, characterName string) string {
	return divisionID + "\x00" + strings.ToLower(characterName)
}

/*
================
ConfigureStyria

ConfigureStyria installs the Styria Clash engine while its switch
is on. Called once at startup, after monster wiring: without the
monster state no instance layer can open, so an unwired runtime
stays off. The schedule itself is read live from rt.Styria
(styriaSettings), which composition fills from the environment.
================
*/
func (rt *Runtime) ConfigureStyria(value string) {
	if !styria.EnabledValue(value) || rt.Monsters == nil {
		return
	}
	rt.styria = &styriaEvent{
		registrants: make(map[string]StyriaRegistrant),
		matches:     make(map[string]*styriaMatch),
		statusSent:  make(map[string][]byte),
	}
}

/*
================
styriaRegistered
================
*/
func (rt *Runtime) styriaRegistered(divisionID, characterName string) bool {
	if rt.styria == nil {
		return false
	}
	rt.styria.mu.Lock()
	defer rt.styria.mu.Unlock()
	_, queued := rt.styria.registrants[styriaKey(divisionID, characterName)]
	return queued
}

/*
================
styriaTalkOptions

The arena manager's talk rows: Apply while the talker holds no
application, Cancel application while they do (the later client's
own already-applied answer offers the same cancel). Every other NPC,
and the whole feature while off, offers nothing.
================
*/
func (rt *Runtime) styriaTalkOptions(divisionID string, character *enterworld.Character, npcCodename string) []NpcQuestOption {
	if rt.styria == nil || character == nil || npcCodename != styria.ManagerCodename {
		return nil
	}
	if rt.styriaRegistered(divisionID, character.Name) {
		return []NpcQuestOption{{
			Codename: styria.ServiceCancel, TitleSymbol: styria.CancelTitleSymbol,
			PromptSymbol: styria.PromptSymbol, Immediate: true,
		}}
	}
	return []NpcQuestOption{{
		Codename: styria.ServiceApply, TitleSymbol: styria.ApplyTitleSymbol,
		PromptSymbol: styria.PromptSymbol, Immediate: true,
	}}
}

/*
================
styriaAnswer

One application answer: a notice over the closing dialog line (the
manager's own greeting). The client slice replaces the notice text
with the later client's arena strings; until then the words are the
server's plain English.
================
*/
func styriaAnswer(text string) []wire.Frame {
	return []wire.Frame{
		wire.NotificationFrame(text),
		{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(styria.ManagerSpeech)},
	}
}

/*
================
styriaApply

The Apply row: queue the talker for the next match, on their suit's
side. The manager's conditions, checked under the division lane on
the character record: participation is open, the talker lives, a job
suit is worn, no party is formed yet (a party forms only after
entering), the talker stands in the open world with no other
programme's return anchor, and no application is already live.
================
*/
func (rt *Runtime) styriaApply(divisionID string, character *enterworld.Character) []wire.Frame {
	if rt.styria == nil || character == nil {
		return styriaAnswer("Styria Clash is not running.")
	}
	if !rt.styriaSettings().participationOpen(rt.Now()) {
		return styriaAnswer("Styria Clash: participation is closed.")
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	var (
		deletePending bool
		alive         bool
		job           uint8
		inParty       bool
		inInstance    bool
	)
	rt.deps.Read(divisionID, func() {
		deletePending = character.DeletePending
		alive = enterworld.CharacterAlive(character)
		job = enterworld.DressedJob(character)
		inParty = rt.rewardPartyOf(divisionID, enterworld.ObjectIDForCharacter(character)) != nil
		inInstance = domain.CharacterWorldInstance(character) != domain.DefaultWorldInstance ||
			(character.World != nil && character.World.InstanceReturn != nil)
	})
	switch {
	case deletePending || !alive:
		return styriaAnswer("Styria Clash: the dead cannot apply.")
	case job == 0:
		return styriaAnswer("Styria Clash: only a character wearing a job suit can apply.")
	case inParty:
		return styriaAnswer("Styria Clash: leave your party first; a party forms only after entering.")
	case inInstance:
		return styriaAnswer("Styria Clash: you cannot apply from inside there.")
	}
	if rt.styriaRegistered(divisionID, character.Name) {
		return styriaAnswer("Styria Clash: you have already applied. Cancel the application to withdraw.")
	}

	side := styriaSideOf(job)
	rt.styria.mu.Lock()
	rt.styria.seq++
	rt.styria.registrants[styriaKey(divisionID, character.Name)] = StyriaRegistrant{
		Division:    divisionID,
		CharacterID: character.ID,
		Name:        character.Name,
		Gid:         enterworld.ObjectIDForCharacter(character),
		Side:        side,
		Seq:         rt.styria.seq,
	}
	rt.styria.mu.Unlock()
	return styriaAnswer(fmt.Sprintf("Styria Clash: application accepted for the %s side.", styriaSideName(side)))
}

/*
================
styriaCancel

The Cancel row: withdraw a live application. Ungated, like the
flag-war register: a registrant who died or changed state after
applying can still leave the queue (the match re-checks admission
at entry anyway).
================
*/
func (rt *Runtime) styriaCancel(divisionID string, character *enterworld.Character) []wire.Frame {
	if rt.styria == nil || character == nil {
		return styriaAnswer("Styria Clash is not running.")
	}
	rt.styria.mu.Lock()
	_, queued := rt.styria.registrants[styriaKey(divisionID, character.Name)]
	if queued {
		delete(rt.styria.registrants, styriaKey(divisionID, character.Name))
	}
	rt.styria.mu.Unlock()
	if !queued {
		return styriaAnswer("Styria Clash: you hold no application.")
	}
	return styriaAnswer("Styria Clash: application cancelled.")
}

/*
================
advanceStyriaClash

The engine's tick step: the start instant forms each division's
match from its queue (styria_match.go), then every live match
advances. Edges fire on the first tick at or after them, so a
stalled tick starts late rather than never. Off, the step is the
nil check.
================
*/
func (rt *Runtime) advanceStyriaClash(nowMs int64) []simulation.DivisionFrames {
	if rt.styria == nil {
		return nil
	}
	event := rt.styria
	event.mu.Lock()
	if !event.ticked {
		event.ticked = true
		event.lastTickMs = nowMs
	}
	previous := event.lastTickMs
	event.lastTickMs = nowMs
	event.mu.Unlock()

	settings := rt.styriaSettings()
	for _, start := range settings.nearStarts(time.UnixMilli(nowMs)) {
		if previous < start.UnixMilli() && start.UnixMilli() <= nowMs {
			rt.styriaStartMatches(start)
		}
	}
	frames := rt.advanceStyriaMatches(nowMs)
	// The client windows' standing (styria_status.go), after the
	// matches so an ending clock's result rides the same tick.
	rt.styriaPushStatuses(nowMs)
	return frames
}

/*
================
styriaStartMatches

The start instant: the queue drains in sign-up order. Per division
the two sides field equally - as many as the smaller side brought,
at least the configured minimum per side - and everyone else is
turned away (their application ends with the window). A division
already fighting keeps its match; its queue still drains, so no
application outlives the window it was made in.
================
*/
func (rt *Runtime) styriaStartMatches(start time.Time) {
	event := rt.styria
	event.mu.Lock()
	registrants := make([]StyriaRegistrant, 0, len(event.registrants))
	for _, registrant := range event.registrants {
		registrants = append(registrants, registrant)
	}
	event.registrants = make(map[string]StyriaRegistrant)
	event.mu.Unlock()
	sort.Slice(registrants, func(i, j int) bool { return registrants[i].Seq < registrants[j].Seq })

	byDivision := make(map[string][]StyriaRegistrant)
	divisions := make([]string, 0, 4)
	for _, registrant := range registrants {
		if _, seen := byDivision[registrant.Division]; !seen {
			divisions = append(divisions, registrant.Division)
		}
		byDivision[registrant.Division] = append(byDivision[registrant.Division], registrant)
	}
	sort.Strings(divisions)

	settings := rt.styriaSettings()
	nowMs := start.UnixMilli()
	for _, division := range divisions {
		queue := byDivision[division]
		event.mu.Lock()
		_, live := event.matches[division]
		event.mu.Unlock()
		if live {
			continue
		}
		var hunters, thieves []StyriaRegistrant
		for _, registrant := range queue {
			if registrant.Side == styriaSideThieves {
				thieves = append(thieves, registrant)
			} else {
				hunters = append(hunters, registrant)
			}
		}
		fielded := len(hunters)
		if len(thieves) < fielded {
			fielded = len(thieves)
		}
		if fielded < settings.MinPerSide {
			continue
		}
		entrants := make([]StyriaRegistrant, 0, 2*fielded)
		entrants = append(entrants, hunters[:fielded]...)
		entrants = append(entrants, thieves[:fielded]...)
		rt.styriaStartMatch(division, entrants, nowMs)
	}
}
