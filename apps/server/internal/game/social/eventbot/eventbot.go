/*
===========================================================================

eventbot.go - the EventBot host: scheduled chat events with rewards

Port-only, not native (#3533, seen in a downstream fork 2026-11-15): the
original has no automated event host and nobody answers to the name
EventBot. The whole host sits behind SRO_EVENT_BOT (off is native): off,
no hook is installed, no tick runs, nothing is announced and a whisper
to "EventBot" is the native cannot-find-target refusal.

The host is only the host: it owns the schedule (a data file of daily
rows), the round timers, entry/answer intake over whispers, winner
selection and the reward grant. What an event IS plugs in per kind
(kinds.go); each event issue brings its own. Rewards go through the
normal item path inside a labelled store door (domain's
GrantEventBotReward), so the item ledger and the free-grant day counts
audit every win; nothing here mints gold.

Abuse posture (owner rule: abuse must cost nothing): a repeat entry or
answer is refused, never counted; a win pays exactly what the data file
says; wins per account per day are capped (bots may play, and one
account may hold several characters - the cap is per account).

===========================================================================
*/
package eventbot

import (
	"crypto/rand"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

// BotName is the chat name the host answers to.
const BotName = "EventBot"

// EnvEventBot is the switch: "on" runs the host, anything else is off.
const EnvEventBot = "SRO_EVENT_BOT"

// EnvEventBotFile names the schedule data file (schedule.go). Unset
// means the host runs with an empty schedule: events start when the
// operator writes the file.
const EnvEventBotFile = "SRO_EVENT_BOT_FILE"

// MaxWinsPerAccountPerDay caps paid wins per account per UTC day, all
// events together. Port-only tuning: free rewards must not scale with
// how many characters or clients one account runs.
const MaxWinsPerAccountPerDay = 3

/*
================
EnabledFromEnv

The eventbot switch ("on", "1", "true"); the same idiom as the other
port-only feature switches (action.UniqueDamageRankFromEnv).
================
*/
func EnabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvEventBot))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
WinsAfterRead

The daily-cap read, fail closed: a failed count counts as the cap
itself, so the account sits the draw out instead of slipping past it
(abuse must cost nothing). WinsToday in the wiring routes its store
read through here.
================
*/
func WinsAfterRead(count int64, err error) int {
	if err != nil {
		return MaxWinsPerAccountPerDay
	}
	return int(count)
}

/*
================
WhisperSender

Who whispered: the live character and its division.
================
*/
type WhisperSender struct {
	Division  string
	Character *enterworld.Character
}

/*
================
Dependencies

The host's doors, all owned by the composition root (wiring_eventbot).
A nil door answers as if the lane held nothing: no outbound line is
sent, no reward is granted, no win is counted.

Whisper and Announce are the outbound chat doors (WhisperToOne /
announce fan-out over the presence directory). Grant pays a reward
into a live character (the caller holds the character's store door;
false = the bag could not take it). WinsToday reads the account's
paid wins on the UTC day containing the given instant from the grant
audit (store grant days). KnownItem validates a reward codename at
schedule load. SpawnHider places a world kind's hider (kinds.go's
WorldKind) and hands back how to remove it; a nil door or a false
answer runs the round hiderless, so nothing can be found. Pick
chooses the winner index (crypto-random in production, fixed in
tests).
================
*/
type Dependencies struct {
	Whisper    func(division, name, text string)
	Announce   func(text string)
	Grant      func(division string, character *enterworld.Character, codename string, count int) bool
	WinsToday  func(accountID string, day time.Time) int
	KnownItem  func(codename string) bool
	SpawnHider func(event Event, spot Spot, at time.Time) (Hider, bool)
	Pick       func(n int) int
	// Now is the host's clock for whisper-side text (the tick brings
	// its own); nil is the wall clock. Tests freeze it.
	Now func() time.Time
}

// entry is one character's place in the live round. The first
// whisper is the entry and, for a judged kind, the answer; repeats
// are refused (host.go), so an entry never changes once placed.
type entry struct {
	accountID string
	name      string
	division  string
	character *enterworld.Character
	answer    string
}

/*
================
Hider

One live hiding NPC in the world, from the spawn door: its object
id (what a finder's selection must name), where it stands, and how
the composition root removes it. The host despawns it at round end
(found, timed out, lost or shut down), exactly once.
================
*/
type Hider struct {
	ObjectID uint32
	RegionID int
	X, Z     float64
	Despawn  func()
}

// liveRound is the round currently taking entries.
type liveRound struct {
	event     Event
	round     int // 1-based
	endsAt    time.Time
	entries   []entry
	byName    map[string]int // character name (folded) -> entries index
	byAccount map[string]int // account -> entries count (this round)
	// A world kind's live play (nil for drawn kinds): the spot in
	// use, the spawned hider and the next hint's time.
	spot     Spot
	hider    *Hider
	nextHint time.Time
}

// occurrenceState tracks one scheduled occurrence across its rounds.
type occurrenceState struct {
	event  Event
	starts time.Time
	round  int // next round to open, 1-based
}

/*
================
Host

The event host. Tick drives the schedule; HandleWhisper takes one
whisper addressed to the bot. Both are called from the composition
root's goroutines (the mission tick and the chat handler), so the
state is mutex-guarded.
================
*/
type Host struct {
	enabled  bool
	schedule Schedule
	deps     Dependencies

	mu      sync.Mutex
	live    *liveRound
	occ     *occurrenceState
	doneDay string
	done    map[string]bool
}

/*
================
New

A host over a parsed schedule. A disabled host is inert: Tick does
nothing and ScheduledEvents lists nothing; the wiring only installs
a disabled host's feed source, never its chat hook.
================
*/
func New(enabled bool, schedule Schedule, deps Dependencies) *Host {
	return &Host{enabled: enabled, schedule: schedule, deps: deps, done: map[string]bool{}}
}

/*
================
now
================
*/
func (h *Host) now() time.Time {
	if h.deps.Now != nil {
		return h.deps.Now()
	}
	return time.Now()
}

/*
================
Enabled
================
*/
func (h *Host) Enabled() bool {
	return h != nil && h.enabled
}

/*
================
pick
================
*/
func (h *Host) pick(n int) int {
	if h.deps.Pick != nil {
		return h.deps.Pick(n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

/*
================
whisperTo

One line to one player; an unwired door drops it.
================
*/
func (h *Host) whisperTo(e entry, text string) {
	if h.deps.Whisper != nil {
		h.deps.Whisper(e.division, e.name, text)
	}
}

/*
================
announce

One line to the whole shard; an unwired door drops it.
================
*/
func (h *Host) announce(text string) {
	if h.deps.Announce != nil {
		h.deps.Announce(text)
	}
}
