/*
===========================================================================

reentry_hold_test.go - a slow re-entry rebuild names its parts

A return-scroll arrival holds the publication gate while ReentryPackets
rebuilds the entry; live publication_holds showed that rebuild
(returnArrival.reentry) as the slow step, up to 215 ms (#2802). These
tests pin that a slow rebuild reports the part that cost the time as
reentry.<part>, and that a calm one reports nothing.

===========================================================================
*/
package enterworld

import (
	"testing"
	"time"
)

/*
================
slowQuestNormalize

Deps whose quest normalisation costs cost on a hand-driven clock, so a
test makes one known part slow without sleeping.
================
*/
func slowQuestNormalize(cost time.Duration) (*Deps, string, map[string]time.Duration) {
	character := chinaSpearman()
	deps := testDeps(character)
	setupReentryBenchCache(deps)
	at := time.Unix(1_800_000_000, 0)
	deps.Now = func() time.Time { return at }
	deps.NormalizeEntryQuests = func(*Character) error {
		at = at.Add(cost)
		return nil
	}
	got := map[string]time.Duration{}
	deps.PublicationHold = func(label string, elapsed time.Duration) { got[label] = elapsed }
	return deps, character.Name, got
}

/*
================
TestSlowReentryNamesItsSlowPart

A 150 ms quest normalisation inside a re-entry reports
reentry.normalize at that cost and no other part, since nothing else
moved the clock.
================
*/
func TestSlowReentryNamesItsSlowPart(t *testing.T) {
	deps, name, got := slowQuestNormalize(150 * time.Millisecond)
	if _, ok := deps.ReentryPackets(DefaultDivisionID, name); !ok {
		t.Fatal("fixture re-entry refused")
	}
	if got["reentry.normalize"] != 150*time.Millisecond || len(got) != 1 {
		t.Fatalf("reported %v, want only reentry.normalize = 150ms", got)
	}
}

/*
================
TestCalmReentryReportsNothing

A re-entry under the slow threshold reports no part at all.
================
*/
func TestCalmReentryReportsNothing(t *testing.T) {
	deps, name, got := slowQuestNormalize(5 * time.Millisecond)
	if _, ok := deps.ReentryPackets(DefaultDivisionID, name); !ok {
		t.Fatal("fixture re-entry refused")
	}
	if len(got) != 0 {
		t.Fatalf("a calm re-entry reported %v", got)
	}
}
