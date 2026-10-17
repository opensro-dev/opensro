/*
===========================================================================

chatfeed_test.go - the website transcript's bound, purge and mute filter

===========================================================================
*/
package chatfeed

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

/*
================
TestFeedKeepsTheLatestLinesInOrder
================
*/
func TestFeedKeepsTheLatestLinesInOrder(t *testing.T) {
	f := New()
	at := time.UnixMilli(1_790_000_000_000)
	f.now = func() time.Time { at = at.Add(time.Second); return at }
	for i := 0; i < MaxLines+3; i++ {
		f.Record("shard", KindChat, "Kekw", "Kekw", fmt.Sprintf("line %d", i))
	}
	f.Record("shard", KindChat, "Kekw", "Kekw", "   ")
	f.Record("other", KindChat, "Lune", "Lune", "elsewhere")
	lines := f.Lines("shard", nil)
	if len(lines) != MaxLines || lines[0].Text != "line 3" || lines[MaxLines-1].Text != fmt.Sprintf("line %d", MaxLines+2) {
		t.Fatalf("lines %+v", lines)
	}
	if !lines[0].At.Before(lines[1].At) {
		t.Fatalf("times out of order: %v then %v", lines[0].At, lines[1].At)
	}
	// The copy is detached: changing it leaves the feed alone.
	lines[0].Text = "changed"
	if f.Lines("shard", nil)[0].Text != "line 3" {
		t.Fatal("Lines returned the feed's own storage")
	}
	if other := f.Lines("other", nil); len(other) != 1 || other[0].Character != "Lune" {
		t.Fatalf("divisions mixed: %+v", other)
	}
}

/*
================
TestBanPurgesAndMuteHides

Purge removes a banned author's lines whatever display name they were
recorded under - the character name, a job alias, an earlier alias - and
keeps everyone else's and the notices. A muted author is left out by
character name, so a line showing a job alias hides too: hidden is only
ever asked about the author, never the display name.
================
*/
func TestBanPurgesAndMuteHides(t *testing.T) {
	f := New()
	f.Record("shard", KindChat, "Remo", "Remo", "spam")
	f.Record("shard", KindNotice, "", "", "Maintenance at 10:00")
	f.Record("shard", KindGlobal, "Lune", "Lune", "wts blade")
	f.Record("shard", KindChat, "REMO", "REMO", "more spam")
	// A suited author's line shows the job alias; the author stays Remo.
	f.Record("shard", KindChat, "Remo", "JobFox", "suited spam")
	muted := map[string]bool{"lune": true}
	var hiddenSaw []string
	hidden := func(author string) bool {
		hiddenSaw = append(hiddenSaw, author)
		return muted[strings.ToLower(author)]
	}
	if lines := f.Lines("shard", hidden); len(lines) != 4 || lines[1].Kind != KindNotice ||
		lines[3].Character != "JobFox" || lines[3].Author != "Remo" {
		t.Fatalf("muted read %+v", lines)
	}
	for _, author := range hiddenSaw {
		if author == "JobFox" {
			t.Fatalf("hidden was asked about the display name: %v", hiddenSaw)
		}
	}
	// Muting the character hides the suited line with the rest.
	muted = map[string]bool{"remo": true}
	if lines := f.Lines("shard", hidden); len(lines) != 2 || lines[0].Kind != KindNotice || lines[1].Character != "Lune" {
		t.Fatalf("muted suited read %+v", lines)
	}
	f.Purge("shard", "remo")
	lines := f.Lines("shard", nil)
	if len(lines) != 2 || lines[0].Kind != KindNotice || lines[1].Character != "Lune" {
		t.Fatalf("after purge %+v", lines)
	}
	f.Record("shard", KindChat, "Kekw", "Kekw", "after")
	if lines := f.Lines("shard", nil); len(lines) != 3 || lines[2].Text != "after" {
		t.Fatalf("record after purge %+v", lines)
	}
}

/*
================
TestNilFeedIsInert

Wiring may leave the feed out; every call on nil is a no-op.
================
*/
func TestNilFeedIsInert(t *testing.T) {
	var f *Feed
	f.Record("shard", KindChat, "Kekw", "Kekw", "hello")
	f.Purge("shard", "Kekw")
	if lines := f.Lines("shard", nil); lines != nil {
		t.Fatalf("nil feed lines %+v", lines)
	}
}

/*
================
TestRecordDropsControlAndBidiRuns
================
*/
func TestRecordDropsControlAndBidiRuns(t *testing.T) {
	f := New()
	f.Record("shard", KindChat, "Kekw", "Kekw", "wts\u202e ssalg\u202c\x00 blade\t\u2067x\u2069")
	f.Record("shard", KindChat, "Kekw", "Kekw", "\u202e\u202c\n")
	lines := f.Lines("shard", nil)
	if len(lines) != 1 || lines[0].Text != "wts ssalg bladex" {
		t.Fatalf("lines %+v", lines)
	}
}
