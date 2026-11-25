/*
===========================================================================

kinds.go - the event plug: what an event is, per kind

The host (eventbot.go) owns timers, intake, winner selection and
rewards; a kind owns only how its rounds read and how a whisper during
a round is judged. Each event issue (#3536 and its siblings) registers
its kind here; the data file names kinds, and an unknown name refuses
the schedule.

The built-in raffle is the reference plug: whispering during a round
enters you, and the winner is drawn from the entrants. A draw, not a
race - the issue's rule that rewards must not pay for scripted speed
alone.

Port-only, not native.

===========================================================================
*/
package eventbot

import (
	"fmt"
	"time"
)

/*
================
Kind

One event plug. OpenLine is announced when a round opens. Judge
scores an entrant's whisper text while the round runs: a judged kind
keeps the first answer (the host refuses repeats), and the winner is
drawn from the entrants whose answer judged correct - never the
fastest, so a script gains nothing. A kind whose Judge always answers
true runs a pure raffle over its entrants.
================
*/
type Kind interface {
	// Name is the data file's word for the kind.
	Name() string
	// Title is the display name when the file names none.
	Title() string
	// OpenLine announces an opening round.
	OpenLine(event Event, round int) string
	// Judge scores one whisper's text for an entrant.
	Judge(text string) bool
}

/*
================
EntryLiner

A kind with its own entry reply; the host's default names the draw
at round close. A seek round is won by reaching the hider, so it
says that instead.
================
*/
type EntryLiner interface {
	// EntryLine answers a player who just entered the round.
	EntryLine(event Event, round int) string
}

/*
================
TicketedKind

A kind whose entry spends an item (Lottery, #3539): before the
first whisper enters, the host takes one of the named item from
the entrant's bag through the ticket door; a bag without one is
refused and nothing is spent. Repeats never reach the door (the
host refuses them first), so an entry costs exactly one ticket.
================
*/
type TicketedKind interface {
	Kind
	// TicketItem is the codename one entry spends.
	TicketItem() string
}

/*
================
WorldKind

A kind whose rounds act in the world while they run (Hide & Seek,
#3536): the host spawns its hider through the spawn door when a
round opens, posts its hints on its own cadence, never draws a
winner for it (the first finder reported to the host wins instead),
and despawns the hider when the round ends however it ends.
ValidateEvent refuses a schedule row the kind cannot run.
================
*/
type WorldKind interface {
	Kind
	// ValidateEvent checks the row's world fields (hider, spots).
	ValidateEvent(event Event) error
	// SpotFor picks the round's hiding spot, deterministically.
	SpotFor(event Event, round int) (Spot, bool)
	// Hint is the hint posted at elapsed into the round. It is built
	// from the spot's name only: coordinates never reach a hint.
	Hint(event Event, spot Spot, elapsed time.Duration) string
	// HintEvery is the hint cadence inside a round.
	HintEvery() time.Duration
}

/*
================
SpotPicker

A world kind whose round's place is drawn, not rotated (Random
Unique, #3537): the host passes its own pick (injected in tests),
so the draw is the host's one random door.
================
*/
type SpotPicker interface {
	// PickSpot draws the round's place from the event's spots.
	PickSpot(event Event, pick func(n int) int) (Spot, bool)
}

/*
================
MonsterPicker

A world kind whose round's monster is drawn from the file's
unique= codenames (Random Unique, #3537). The host records the
pick on the live event's Hider field before the spawn door runs,
so the door and the hints name the monster that actually stands.
================
*/
type MonsterPicker interface {
	// PickMonster draws the round's monster codename.
	PickMonster(event Event, pick func(n int) int) (string, bool)
}

/*
================
KillKind

A world kind won by slaying its monster (Random Unique, #3537),
not by reaching it: the composition root reports the kill owner to
the host (ReportSlain) and reaching the monster wins nothing.
================
*/
type KillKind interface {
	WorldKind
	// WonByKill says the round is won by a kill.
	WonByKill() bool
}

// kinds is the registry, by data-file name.
var kinds = map[string]Kind{}

/*
================
RegisterKind

Adds a kind to the registry. Called from an event's own init/wiring;
a duplicate name is a build defect and panics, like a duplicate opcode.
================
*/
func RegisterKind(kind Kind) {
	if _, dup := kinds[kind.Name()]; dup {
		panic("eventbot: duplicate event kind " + kind.Name())
	}
	kinds[kind.Name()] = kind
}

/*
================
kindByName
================
*/
func kindByName(name string) (Kind, bool) {
	kind, ok := kinds[name]
	return kind, ok
}

// raffleKind is the built-in draw.
type raffleKind struct{}

/*
================
Name
================
*/
func (raffleKind) Name() string { return "raffle" }

/*
================
Title
================
*/
func (raffleKind) Title() string { return "Raffle" }

/*
================
OpenLine
================
*/
func (raffleKind) OpenLine(event Event, round int) string {
	return fmt.Sprintf("%s, round %d of %d: whisper %s in the next %s to take part - the winner is drawn.",
		event.DisplayTitle(), round, event.Rounds, BotName, event.RoundLength)
}

/*
================
Judge

Every entrant is in the draw.
================
*/
func (raffleKind) Judge(string) bool { return true }

func init() {
	RegisterKind(raffleKind{})
}
