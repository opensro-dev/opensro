/*
===========================================================================

chatfeed_test.go - the website transcript's bound, purge and mute filter

===========================================================================
*/
package chatfeed

import (
	"fmt"
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
		f.Record("shard", KindChat, "Kekw", fmt.Sprintf("line %d", i))
	}
	f.Record("shard", KindChat, "Kekw", "   ")
	f.Record("other", KindChat, "Lune", "elsewhere")
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

Purge removes a banned author's lines whatever the name's case and keeps
everyone else's and the notices; a muted author is left out only while
hidden says so.
================
*/
func TestBanPurgesAndMuteHides(t *testing.T) {
	f := New()
	f.Record("shard", KindChat, "Remo", "spam")
	f.Record("shard", KindNotice, "", "Maintenance at 10:00")
	f.Record("shard", KindGlobal, "Lune", "wts blade")
	f.Record("shard", KindChat, "REMO", "more spam")
	muted := map[string]bool{"Lune": true}
	if lines := f.Lines("shard", func(name string) bool { return muted[name] }); len(lines) != 3 || lines[1].Kind != KindNotice {
		t.Fatalf("muted read %+v", lines)
	}
	f.Purge("shard", "remo")
	lines := f.Lines("shard", nil)
	if len(lines) != 2 || lines[0].Kind != KindNotice || lines[1].Character != "Lune" {
		t.Fatalf("after purge %+v", lines)
	}
	f.Record("shard", KindChat, "Kekw", "after")
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
	f.Record("shard", KindChat, "Kekw", "hello")
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
	f.Record("shard", KindChat, "Kekw", "wts\u202e ssalg\u202c\x00 blade\t\u2067x\u2069")
	f.Record("shard", KindChat, "Kekw", "\u202e\u202c\n")
	lines := f.Lines("shard", nil)
	if len(lines) != 1 || lines[0].Text != "wts ssalg bladex" {
		t.Fatalf("lines %+v", lines)
	}
}
