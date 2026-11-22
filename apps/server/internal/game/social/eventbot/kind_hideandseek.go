/*
===========================================================================

kind_hideandseek.go - Hide & Seek: the first finder wins (#3536)

Port-only, not native (on the EventBot host, #3533): a hider NPC
stands at one of the data file's spots and the bot posts hints; the
first entered player the composition root finds targeting the hider
wins (host.ReportFound). A round nobody finds ends with no winner -
this kind never draws, so standing near the hider scores nothing
and only reaching it counts.

The file row names the hider's codename and its spots:

	20:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_EVENT_HIDER spot=West_Gate|2499|100|200 title=Hide & Seek

Hints are built from the spot's NAME only, so exact coordinates can
never reach one. Spots rotate by round.

===========================================================================
*/
package eventbot

import (
	"fmt"
	"time"
)

// hideSeekHintEvery is the hint cadence inside a round: the opening
// hint plus one every two minutes of a ten-minute round.
const hideSeekHintEvery = 2 * time.Minute

// hideSeekKind is the Hide & Seek plug (kinds.go's WorldKind).
type hideSeekKind struct{}

/*
================
Name
================
*/
func (hideSeekKind) Name() string { return "hideandseek" }

/*
================
Title
================
*/
func (hideSeekKind) Title() string { return "Hide & Seek" }

/*
================
OpenLine
================
*/
func (hideSeekKind) OpenLine(event Event, round int) string {
	return fmt.Sprintf("%s, round %d of %d: a hider is hiding! Whisper %s to enter, then find the hider and target it - the first to reach it wins.",
		event.DisplayTitle(), round, event.Rounds, BotName)
}

/*
================
EntryLine
================
*/
func (hideSeekKind) EntryLine(event Event, round int) string {
	return fmt.Sprintf("You are hunting in %s round %d! Find the hider and target it - the first to reach it wins.",
		event.DisplayTitle(), round)
}

/*
================
Judge

Entering is all a whisper does here; the world decides the winner,
so every entrant's text judges true and none is ever drawn.
================
*/
func (hideSeekKind) Judge(string) bool { return true }

/*
================
ValidateEvent

A seek row needs its hider's codename and at least one whole spot.
================
*/
func (hideSeekKind) ValidateEvent(event Event) error {
	if event.Hider == "" {
		return fmt.Errorf("hideandseek has no hider= codename")
	}
	if len(event.Spots) == 0 {
		return fmt.Errorf("hideandseek has no spot= hiding places")
	}
	for _, spot := range event.Spots {
		if spot.Name == "" || spot.RegionID < 1 || spot.RegionID > 65535 {
			return fmt.Errorf("hideandseek spot %+v is incomplete", spot)
		}
	}
	return nil
}

/*
================
SpotFor

The round's hiding place: the file's spots in order, round after
round (deterministic, so a restart hides the same sequence).
================
*/
func (hideSeekKind) SpotFor(event Event, round int) (Spot, bool) {
	if len(event.Spots) == 0 || round < 1 {
		return Spot{}, false
	}
	return event.Spots[(round-1)%len(event.Spots)], true
}

/*
================
Hint

Says where to search, never where the hider stands: the spot's name
is the whole hint.
================
*/
func (hideSeekKind) Hint(event Event, spot Spot, elapsed time.Duration) string {
	if elapsed <= 0 {
		return fmt.Sprintf("First hint: the hider waits near %s. Whisper %s to enter the hunt!", spot.DisplayName(), BotName)
	}
	return fmt.Sprintf("The hider is still near %s - keep searching!", spot.DisplayName())
}

/*
================
HintEvery
================
*/
func (hideSeekKind) HintEvery() time.Duration { return hideSeekHintEvery }

func init() {
	RegisterKind(hideSeekKind{})
}
