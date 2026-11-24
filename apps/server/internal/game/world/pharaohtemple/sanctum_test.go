/*
===========================================================================

sanctum_test.go - the sanctum gate table agrees with the dungeons and the
schedules open each pair when the shard's rows say

===========================================================================
*/
package pharaohtemple

import (
	"testing"
	"time"
)

/*
================
TestSanctumsMatchTheirDungeons

Every sanctum is one of the six sanctum dungeons, under the source world
the dungeon table groups it by, and its gates join its own region to the
Temple: in through the enter gate's target, out through the leave gate.
================
*/
func TestSanctumsMatchTheirDungeons(t *testing.T) {
	byRegion := map[uint16]Dungeon{}
	for _, dungeon := range Dungeons() {
		byRegion[dungeon.Region] = dungeon
	}
	seen := map[uint16]bool{}
	for _, sanctum := range Sanctums() {
		dungeon, ok := byRegion[sanctum.Region]
		if !ok || dungeon.ID == TempleDungeonID {
			t.Fatalf("%s: region %#x is not a sanctum dungeon", sanctum.Group, sanctum.Region)
		}
		if dungeon.SourceWorld != sanctum.SourceWorld {
			t.Errorf("%s: source world %d, dungeon table says %d", sanctum.Group, sanctum.SourceWorld, dungeon.SourceWorld)
		}
		if seen[sanctum.Region] {
			t.Errorf("%s: region %#x listed twice", sanctum.Group, sanctum.Region)
		}
		seen[sanctum.Region] = true
		if sanctum.EnterTarget.Region != sanctum.Region || sanctum.Leave.Region != sanctum.Region {
			t.Errorf("%s: gates do not lead into and out of %#x", sanctum.Group, sanctum.Region)
		}
		if sanctum.LeaveTarget.Region != TempleRegionID {
			t.Errorf("%s: leave gate leads to %#x, not the Temple", sanctum.Group, sanctum.LeaveTarget.Region)
		}
	}
	if len(seen) != LastDungeonID-FirstDungeonID {
		t.Fatalf("%d sanctums, want %d", len(seen), LastDungeonID-FirstDungeonID)
	}
}

/*
================
TestSethOpensFromHaroerisSanctum

Five enter gates stand in the Temple and open with their schedule; Seth's
stands in Haroeris's sanctum and opens when Haroeris dies.
================
*/
func TestSethOpensFromHaroerisSanctum(t *testing.T) {
	var haroeris Sanctum
	for _, sanctum := range Sanctums() {
		if sanctum.Keeper == "MOB_SD_HAROERIS" {
			haroeris = sanctum
		}
	}
	for _, sanctum := range Sanctums() {
		if sanctum.Keeper != "MOB_SD_SETH" {
			if sanctum.Enter.Region != TempleRegionID || sanctum.OpenedBy != "" {
				t.Errorf("%s: enter gate in %#x opened by %q", sanctum.Group, sanctum.Enter.Region, sanctum.OpenedBy)
			}
			continue
		}
		if sanctum.Enter.Region != haroeris.Region || sanctum.OpenedBy != haroeris.Keeper {
			t.Errorf("Seth: enter gate in %#x opened by %q, want Haroeris's %#x", sanctum.Enter.Region, sanctum.OpenedBy, haroeris.Region)
		}
		if sanctum.Schedule != haroeris.Schedule {
			t.Errorf("Seth rides schedule %d, Haroeris %d", sanctum.Schedule, haroeris.Schedule)
		}
	}
}

/*
================
TestSanctumWindows

Each pair's two daily windows run two hours from the copy1 starts: the
gates open half an hour in, after notices at 30, 10 and 5 minutes left.
================
*/
func TestSanctumWindows(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 11, 24, 0, 0, 0, 0, loc)
	cases := []struct {
		define int
		at     time.Duration
		in     bool
		start  time.Duration
	}{
		{ScheduleHaroerisSeth, 3*time.Hour + 10*time.Minute, true, 3 * time.Hour},
		{ScheduleHaroerisSeth, 5*time.Hour + time.Minute, false, 0},
		{ScheduleHaroerisSeth, 14*time.Hour + 29*time.Minute, true, 12*time.Hour + 30*time.Minute},
		{ScheduleSelkisNeith, 30 * time.Minute, true, 0},
		{ScheduleSelkisNeith, 10 * time.Hour, true, 9*time.Hour + 30*time.Minute},
		{ScheduleAnubisIsis, time.Hour, false, 0},
		{ScheduleAnubisIsis, 2 * time.Hour, true, time.Hour + 30*time.Minute},
	}
	for _, c := range cases {
		in := false
		var start time.Time
		for _, window := range Windows(c.define) {
			if s, _, ok := window.Current(day.Add(c.at), loc); ok {
				in, start = true, s
			}
		}
		if in != c.in || in && !start.Equal(day.Add(c.start)) {
			t.Errorf("define %d at %v: in=%v from %v, want in=%v from %v", c.define, c.at, in, start, c.in, day.Add(c.start))
		}
	}
	if Windows(99) != nil {
		t.Error("an unknown define has windows")
	}
	offsets := NoticeOffsets()
	if len(offsets) != 3 || OpenAfter-offsets[0] != 30*time.Minute || OpenAfter-offsets[1] != 10*time.Minute ||
		OpenAfter-offsets[2] != 5*time.Minute {
		t.Errorf("notices %v before an open at %v", offsets, OpenAfter)
	}
}
