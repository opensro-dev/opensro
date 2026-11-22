/*
===========================================================================

honorfeed_test.go - the Job Honor ranking feed (#3530)

A session's first poll sends its snapshot; an unchanged board sends
nothing until the next poll and nothing when the board is unchanged;
a hidden alias drops its row from the board and the viewer's place;
the own standing follows the viewer's strongest standing.

===========================================================================
*/
package honorfeed

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/platform/jobhonor"
)

// feedNow is the frozen instant every fake reads.
var feedNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// fakeFeed is the feed's doors with swappable standings.
type fakeFeed struct {
	current  []jobhonor.Standing
	previous []jobhonor.Standing
	end      time.Time
	used     int
	points   map[int64]map[int]int64
}

func (f *fakeFeed) sources() Sources {
	return Sources{
		Current:  func() ([]jobhonor.Standing, error) { return f.current, nil },
		Previous: func() ([]jobhonor.Standing, time.Time, error) { return f.previous, f.end, nil },
		Alias: func(characterID int64) (string, bool) {
			switch characterID {
			case 1:
				return "Alpha", true
			case 2:
				return "Beta", true
			}
			return "", false
		},
		Account: func(characterID int64) (string, bool) {
			if characterID == 1 {
				return "acct-one", true
			}
			return "acct", true
		},
		Points: func(characterID int64, from, to time.Time) (map[int]int64, error) {
			return f.points[characterID], nil
		},
		Used: func(string) int { return f.used },
		Now:  func() time.Time { return feedNow },
	}
}

// snapshotTail reads the own block at the end of a snapshot payload.
func snapshotTail(t *testing.T, payload []byte) (place, capLeft int) {
	t.Helper()
	// header: 3 x u64 + u16 count
	off := 24
	count := int(binary.LittleEndian.Uint16(payload[off:]))
	off += 2
	for period := 0; period < 2; period++ {
		for i := 0; i < count; i++ {
			off++ // job
			n := int(payload[off])
			off += 1 + n + 4 + 2 // alias, points, tiers
		}
		if period == 0 {
			count = int(binary.LittleEndian.Uint16(payload[off:]))
			off += 2
		}
	}
	if payload[off] != 1 {
		t.Fatalf("own present = %d, want 1", payload[off])
	}
	off++
	off++ // job
	place = int(binary.LittleEndian.Uint16(payload[off:]))
	off += 2
	off += 2 // tiers
	capLeft = int(binary.LittleEndian.Uint32(payload[off:]))
	return place, capLeft
}

func TestTickSendsOnFirstSightAndOnChange(t *testing.T) {
	f := &fakeFeed{
		current: []jobhonor.Standing{
			{CharacterID: 1, Job: jobhonor.JobTrader, Points: 300},
			{CharacterID: 2, Job: jobhonor.JobThief, Points: 100},
			{CharacterID: 3, Job: jobhonor.JobThief, Points: 999},
		},
		end:    feedNow.AddDate(0, 0, -7),
		used:   250,
		points: map[int64]map[int]int64{1: {jobhonor.JobTrader: 40}},
	}
	feed := New(f.sources())
	sessions := []Session{{ID: "s1", CharacterID: 1}}
	out := feed.Tick(0, sessions)
	payload, sent := out["s1"]
	if !sent {
		t.Fatal("the first poll sent nothing")
	}
	// The hidden character (no alias) holds no row: the viewer's
	// 300 points rank first even though 999 were scored invisibly,
	// and the account's 250 kept points leave 750 of the cap.
	if place, capLeft := snapshotTail(t, payload); place != 1 || capLeft != jobhonor.DailyCap-250 {
		t.Fatalf("own place/capLeft = %d/%d, want 1/%d", place, capLeft, jobhonor.DailyCap-250)
	}
	// Between polls nothing is composed.
	if out := feed.Tick(1, sessions); out != nil {
		t.Fatal("a tick inside the poll interval sent a snapshot")
	}
	// The same board at the next poll sends nothing.
	if out := feed.Tick(pollIntervalMs, sessions); len(out) != 0 {
		t.Fatal("an unchanged board resent its snapshot")
	}
	// A moved board resends.
	f.current[1].Points = 400
	if out := feed.Tick(2*pollIntervalMs, sessions); out["s1"] == nil {
		t.Fatal("a changed board sent nothing")
	}
	// A session first seen late gets its own snapshot; the existing
	// one is not disturbed.
	out = feed.Tick(3*pollIntervalMs, []Session{{ID: "s1", CharacterID: 1}, {ID: "s2", CharacterID: 2}})
	if out["s2"] == nil || out["s1"] != nil {
		t.Fatal("a new session did not get a first snapshot")
	}
	// A vanished session is forgotten: it is a first sight again.
	feed.Tick(4*pollIntervalMs, sessions)
	out = feed.Tick(5*pollIntervalMs, []Session{{ID: "s1", CharacterID: 1}, {ID: "s2", CharacterID: 2}})
	if out["s2"] == nil {
		t.Fatal("a returned session was not treated as a first sight")
	}
}
