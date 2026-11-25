/*
===========================================================================

kind_randomunique.go - Random Unique: the killer takes the prize (#3537)

Port-only, not native (on the EventBot host, #3533): one of the data
file's unique monsters appears at one of its spots for the round's
window; entered players hunt it, and the one who lands the killing
blow takes the file's reward through the grant door (host.ReportSlain).
A unique that outlives its window leaves with no winner, and a kill
by a player who never entered - or by an account at its daily win
cap - pays nobody: the monster is gone either way, so the round ends
with the kill. Reaching or targeting the unique wins nothing; only
the killing blow counts, so a script gains nothing by standing close.

The file row names the unique codenames and the spots:

	21:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_RM_TAHOMET spot=Hotan|2500|100|200 title=Random Unique

The monster and the spot are drawn per round through the host's
pick (kinds.go's pickers); hints name the spot only, never its
coordinates.

===========================================================================
*/
package eventbot

import (
	"fmt"
	"time"
)

// uniqueHintEvery is the hint cadence inside a round: the opening
// hint plus one every five minutes of a thirty-minute window.
const uniqueHintEvery = 5 * time.Minute

// uniqueKind is the Random Unique plug (kinds.go's KillKind).
type uniqueKind struct{}

/*
================
Name
================
*/
func (uniqueKind) Name() string { return "randomunique" }

/*
================
Title
================
*/
func (uniqueKind) Title() string { return "Random Unique" }

/*
================
OpenLine
================
*/
func (uniqueKind) OpenLine(event Event, round int) string {
	return fmt.Sprintf("%s, round %d of %d: a unique has appeared! Whisper %s to enter, then slay it before the window closes - the killer takes the prize.",
		event.DisplayTitle(), round, event.Rounds, BotName)
}

/*
================
EntryLine
================
*/
func (uniqueKind) EntryLine(event Event, round int) string {
	return fmt.Sprintf("You are hunting in %s round %d! Slay the unique before the window closes - the killer takes the prize.",
		event.DisplayTitle(), round)
}

/*
================
Judge

Entering is all a whisper does here; the world decides the winner,
so every entrant's text judges true and none is ever drawn.
================
*/
func (uniqueKind) Judge(string) bool { return true }

/*
================
WonByKill
================
*/
func (uniqueKind) WonByKill() bool { return true }

/*
================
ValidateEvent

A unique row needs at least one monster codename and one whole
spot: the round draws one of each.
================
*/
func (uniqueKind) ValidateEvent(event Event) error {
	if len(event.Uniques) == 0 {
		return fmt.Errorf("randomunique has no unique= monster codename")
	}
	if len(event.Spots) == 0 {
		return fmt.Errorf("randomunique has no spot= hunting grounds")
	}
	for _, spot := range event.Spots {
		if spot.Name == "" || spot.RegionID < 1 || spot.RegionID > 65535 {
			return fmt.Errorf("randomunique spot %+v is incomplete", spot)
		}
	}
	return nil
}

/*
================
SpotFor

The deterministic fallback (the host prefers PickSpot): the file's
spots in order, round after round.
================
*/
func (uniqueKind) SpotFor(event Event, round int) (Spot, bool) {
	if len(event.Spots) == 0 || round < 1 {
		return Spot{}, false
	}
	return event.Spots[(round-1)%len(event.Spots)], true
}

/*
================
PickSpot

The round's hunting ground, drawn from the file's spots.
================
*/
func (uniqueKind) PickSpot(event Event, pick func(n int) int) (Spot, bool) {
	if len(event.Spots) == 0 {
		return Spot{}, false
	}
	return event.Spots[pick(len(event.Spots))], true
}

/*
================
PickMonster

The round's unique, drawn from the file's unique= codenames.
================
*/
func (uniqueKind) PickMonster(event Event, pick func(n int) int) (string, bool) {
	if len(event.Uniques) == 0 {
		return "", false
	}
	return event.Uniques[pick(len(event.Uniques))], true
}

/*
================
Hint

Names the hunting ground only: the spot's name is the whole hint,
so exact coordinates can never reach one.
================
*/
func (uniqueKind) Hint(event Event, spot Spot, elapsed time.Duration) string {
	if elapsed <= 0 {
		return fmt.Sprintf("The unique waits near %s! Whisper %s to enter the hunt - slay it before the window closes.", spot.DisplayName(), BotName)
	}
	return fmt.Sprintf("The unique still stands near %s - keep hunting!", spot.DisplayName())
}

/*
================
HintEvery
================
*/
func (uniqueKind) HintEvery() time.Duration { return uniqueHintEvery }

func init() {
	RegisterKind(uniqueKind{})
}
