/*
===========================================================================

attendancefeed_test.go - first sight, changed bytes only, the day turn

===========================================================================
*/
package attendancefeed

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/platform/attendance"
)

type fakeLedger struct {
	months map[int64]attendance.Month
	fail   bool
}

func (f *fakeLedger) month(characterID int64) (attendance.Month, error) {
	if f.fail {
		return attendance.Month{}, errors.New("read failed")
	}
	return f.months[characterID], nil
}

func newFeed(ledger *fakeLedger, at *time.Time) *Service {
	return New(Sources{Month: ledger.month, Now: func() time.Time { return *at }})
}

/*
================
TestTickSendsOnFirstSightThenOnlyChanges

Every session gets the empty month on first sight; a poll inside the
interval sends nothing; the clock alone moving sends nothing; a
check-in sends that session's month and no one else's.
================
*/
func TestTickSendsOnFirstSightThenOnlyChanges(t *testing.T) {
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	ledger := &fakeLedger{months: map[int64]attendance.Month{}}
	feed := newFeed(ledger, &at)
	sessions := []Session{{ID: "a", CharacterID: 1}, {ID: "b", CharacterID: 2}}

	out := feed.Tick(0, sessions)
	if len(out) != 2 {
		t.Fatalf("first sight sent %d", len(out))
	}
	payload := out["a"]
	if key := binary.LittleEndian.Uint32(payload); key != 202611 {
		t.Fatalf("month key %d", key)
	}
	if payload[4] != 26 || payload[5] != 30 || binary.LittleEndian.Uint32(payload[6:]) != 0 {
		t.Fatalf("today/days/checked % x", payload[4:10])
	}
	if out := feed.Tick(pollIntervalMs-1, sessions); out != nil {
		t.Fatal("polled inside the interval")
	}
	at = at.Add(time.Minute)
	if out := feed.Tick(pollIntervalMs, sessions); out != nil {
		t.Fatalf("a clock-only change sent %d", len(out))
	}

	ledger.months[1] = attendance.Month{Key: 202611, Checked: 1 << 25}
	out = feed.Tick(2*pollIntervalMs, sessions)
	if len(out) != 1 || out["a"] == nil {
		t.Fatalf("check-in sent %v", out)
	}
	if got := binary.LittleEndian.Uint32(out["a"][6:]); got != 1<<25 {
		t.Fatalf("checked bits %#x", got)
	}
}

/*
================
TestTickTurnsTheDayAndForgets

The 04:00 day turn re-sends with the new day; a stale month key from the
ledger reads as empty; a vanished session is forgotten and gets a fresh
first sight; a failed read sends nothing.
================
*/
func TestTickTurnsTheDayAndForgets(t *testing.T) {
	at := time.Date(2026, 12, 1, 3, 59, 0, 0, time.UTC)
	ledger := &fakeLedger{months: map[int64]attendance.Month{1: {Key: 202611, Checked: 1 << 29}}}
	feed := newFeed(ledger, &at)
	sessions := []Session{{ID: "a", CharacterID: 1}}

	out := feed.Tick(0, sessions)
	if out["a"][4] != 30 || binary.LittleEndian.Uint32(out["a"][6:]) != 1<<29 {
		t.Fatalf("last day of November % x", out["a"][:10])
	}
	at = time.Date(2026, 12, 1, 4, 0, 0, 0, time.UTC)
	out = feed.Tick(pollIntervalMs, sessions)
	if binary.LittleEndian.Uint32(out["a"]) != 202612 || out["a"][4] != 1 || out["a"][5] != 31 {
		t.Fatalf("first of December % x", out["a"][:6])
	}
	if binary.LittleEndian.Uint32(out["a"][6:]) != 0 {
		t.Fatal("November's bits leaked into December")
	}

	if out := feed.Tick(2*pollIntervalMs, nil); out != nil || len(feed.sent) != 0 {
		t.Fatal("a vanished session was kept")
	}
	ledger.fail = true
	if out := feed.Tick(3*pollIntervalMs, sessions); out != nil {
		t.Fatal("a failed read sent a snapshot")
	}
	ledger.fail = false
	if out := feed.Tick(4*pollIntervalMs, sessions); out["a"] == nil {
		t.Fatal("a returning session got no first sight")
	}
}
