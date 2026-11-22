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

import "fmt"

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
