/*
===========================================================================

character_online_test.go - the time-online crediting rules

===========================================================================
*/
package domain

import (
	"testing"
	"time"
)

/*
================
onlineTestMs

A fixed UTC instant for the crediting tests: 2026-11-05 23:59:30 UTC,
thirty seconds before a midnight split.
================
*/
func onlineTestMs() int64 {
	return time.Date(2026, 11, 5, 23, 59, 30, 0, time.UTC).UnixMilli()
}

/*
================
TestCreditOnlineSplitsAtUTCMidnight
================
*/
func TestCreditOnlineSplitsAtUTCMidnight(t *testing.T) {
	start := onlineTestMs()
	c := &Character{}
	if !c.AnchorOnline(start) || c.Online == nil {
		t.Fatal("anchor did not create the record")
	}
	// Ninety seconds from 23:59:30: 30s on the 5th, 60s on the 6th.
	if !c.CreditOnline(start + 90_000) {
		t.Fatal("credit reported no change")
	}
	if c.Online.TotalSeconds != 90 {
		t.Fatalf("total = %d, want 90", c.Online.TotalSeconds)
	}
	if len(c.Online.Days) != 2 {
		t.Fatalf("days = %v, want two buckets", c.Online.Days)
	}
	if c.Online.Days[0] != (OnlineDay{Day: 20261105, Seconds: 30}) {
		t.Fatalf("first bucket = %v", c.Online.Days[0])
	}
	if c.Online.Days[1] != (OnlineDay{Day: 20261106, Seconds: 60}) {
		t.Fatalf("second bucket = %v", c.Online.Days[1])
	}
	if got := c.Online.WeekSeconds(); got != 90 {
		t.Fatalf("week = %d, want 90", got)
	}
}

/*
================
TestCreditOnlineNeverCountsTheSameSpanTwice

Two credit calls over the same span - the evicted/duplicate-session
case - credit it once: the second call measures from the moved anchor.
The sub-second remainder survives in the anchor instead of vanishing.
================
*/
func TestCreditOnlineNeverCountsTheSameSpanTwice(t *testing.T) {
	start := onlineTestMs()
	c := &Character{}
	c.AnchorOnline(start)
	if !c.CreditOnline(start + 61_500) {
		t.Fatal("first credit reported no change")
	}
	if c.Online.TotalSeconds != 61 {
		t.Fatalf("total = %d, want 61", c.Online.TotalSeconds)
	}
	if c.CreditOnline(start + 61_500) {
		t.Fatal("second credit over the same span changed the record")
	}
	if c.Online.TotalSeconds != 61 {
		t.Fatalf("total after duplicate credit = %d, want 61", c.Online.TotalSeconds)
	}
	// The 500ms remainder is still owed: half a second later one more
	// whole second lands.
	if !c.CreditOnline(start + 62_000) {
		t.Fatal("remainder credit reported no change")
	}
	if c.Online.TotalSeconds != 62 {
		t.Fatalf("total = %d, want 62", c.Online.TotalSeconds)
	}
}

/*
================
TestAnchorOnlineRestartsTheClockWithoutCrediting

A fresh login after a crash anchors at the new login: the un-credited
tail of the dead session is dropped, never credited late.
================
*/
func TestAnchorOnlineRestartsTheClockWithoutCrediting(t *testing.T) {
	start := onlineTestMs()
	c := &Character{}
	c.AnchorOnline(start)
	c.AnchorOnline(start + 600_000)
	if c.Online.TotalSeconds != 0 {
		t.Fatalf("anchor credited %d seconds", c.Online.TotalSeconds)
	}
	if !c.CreditOnline(start+660_000) || c.Online.TotalSeconds != 60 {
		t.Fatalf("total = %d, want 60 from the new anchor", c.Online.TotalSeconds)
	}
}

/*
================
TestCreditOnlinePrunesBucketsOlderThanTheWeek
================
*/
func TestCreditOnlinePrunesBucketsOlderThanTheWeek(t *testing.T) {
	start := onlineTestMs()
	c := &Character{Online: &OnlineRecord{
		TotalSeconds: 3600,
		Days: []OnlineDay{
			{Day: 20261001, Seconds: 3600},
			{Day: 20261105, Seconds: 10},
		},
		LastCreditMs: start,
	}}
	if !c.CreditOnline(start + 60_000) {
		t.Fatal("credit reported no change")
	}
	for _, day := range c.Online.Days {
		if day.Day == 20261001 {
			t.Fatalf("stale bucket survived: %v", c.Online.Days)
		}
	}
	// The total is all-time: pruning a bucket never lowers it.
	if c.Online.TotalSeconds != 3660 {
		t.Fatalf("total = %d, want 3660", c.Online.TotalSeconds)
	}
	if got := c.Online.WeekSeconds(); got != 70 {
		t.Fatalf("week = %d, want 70", got)
	}
}

/*
================
TestCreditOnlineIgnoresSubSecondAndAbsentRecords
================
*/
func TestCreditOnlineIgnoresSubSecondAndAbsentRecords(t *testing.T) {
	start := onlineTestMs()
	c := &Character{}
	if c.CreditOnline(start + 60_000) {
		t.Fatal("credit without a record changed something")
	}
	c.AnchorOnline(start)
	if c.CreditOnline(start + 999) {
		t.Fatal("sub-second credit changed the record")
	}
	if c.Online.TotalSeconds != 0 {
		t.Fatalf("total = %d, want 0", c.Online.TotalSeconds)
	}
}
