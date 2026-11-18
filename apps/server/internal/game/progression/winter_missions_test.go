/*
===========================================================================

winter_missions_test.go - the Server Missions' counting against the
native rows and the v1.188 dump's live state

===========================================================================
*/
package progression

import "testing"

/*
================
TestWinterMissionGoalsFollowOnlinePlayers

A per-player goal is PerUser x online + 100 (the dump's 30001 goal 604 =
18 x 28 + 100, 30002's 6580 = 360 x 18 + 100); the summon goals are fixed.
================
*/
func TestWinterMissionGoalsFollowOnlinePlayers(t *testing.T) {
	m := NewWinterMissions(nil, 28, nil)
	want := map[uint32]int{WinterMissionSnowFlake: 604, WinterMissionSnowSlave: 10180, WinterMissionKnights: 1, WinterMissionPrincesses: 5}
	for _, row := range m.Snapshot() {
		if row.Goal != want[row.ID] {
			t.Fatalf("mission %d goal %d, want %d", row.ID, row.Goal, want[row.ID])
		}
		if row.Gauge != (row.ID == WinterMissionSnowFlake) {
			t.Fatalf("mission %d gauge %v", row.ID, row.Gauge)
		}
	}
}

/*
================
TestWinterMissionKillsChainToTheSummons

Each Snow Slave Hunting completion summons the knights once; every fifth
the princesses. The completing hunter is the winner; the chained missions
have none. The remainder carries over and the goal is set again.
================
*/
func TestWinterMissionKillsChainToTheSummons(t *testing.T) {
	m := NewWinterMissions(nil, 0, nil)
	for range 99 {
		if got := m.Record(WinterTriggerKill, 1, "hunter", 0); len(got) != 0 {
			t.Fatalf("completed before the goal: %+v", got)
		}
	}
	got := m.Record(WinterTriggerKill, 1, "last", 2)
	if len(got) != 2 || got[0] != (WinterMissionCompletion{ID: WinterMissionSnowSlave, Times: 1, Winner: "last"}) ||
		got[1] != (WinterMissionCompletion{ID: WinterMissionKnights, Times: 1}) {
		t.Fatalf("completions %+v", got)
	}
	if row := m.Snapshot()[1]; row.Goal != 820 || row.Count != 0 || row.Completions != 1 {
		t.Fatalf("snow slave row after completion %+v", row)
	}
	// Four more completions at once (an overflow): the fifth knights
	// completion summons the princesses.
	got = m.Record(WinterTriggerKill, 4*820+3, "big", 0)
	if len(got) != 3 || got[0].Times != 4 || got[1] != (WinterMissionCompletion{ID: WinterMissionKnights, Times: 4}) ||
		got[2] != (WinterMissionCompletion{ID: WinterMissionPrincesses, Times: 1}) {
		t.Fatalf("overflow completions %+v", got)
	}
	if row := m.Snapshot()[1]; row.Count != 3 || row.Goal != 100 {
		t.Fatalf("carried remainder %+v", row)
	}
}

/*
================
TestWinterMissionTriggersStaySeparate

A kill never counts toward the Snow Flake Event, an exchange never toward
the hunt.
================
*/
func TestWinterMissionTriggersStaySeparate(t *testing.T) {
	m := NewWinterMissions(nil, 0, nil)
	m.Record(WinterTriggerKill, 50, "a", 0)
	m.Record(WinterTriggerExchange, 7, "b", 0)
	rows := m.Snapshot()
	if rows[0].Count != 7 || rows[1].Count != 50 || rows[2].Count != 0 {
		t.Fatalf("rows %+v", rows)
	}
}

/*
================
TestWinterMissionsResumeKeptRows

A kept row resumes as kept; a damaged one starts over.
================
*/
func TestWinterMissionsResumeKeptRows(t *testing.T) {
	kept := []WinterMissionState{
		{ID: WinterMissionSnowSlave, Completions: 63, Goal: 6580, Count: 2038},
		{ID: WinterMissionPrincesses, Completions: 12, Goal: 0, Count: 3},
	}
	rows := NewWinterMissions(kept, 1, nil).Snapshot()
	if rows[1] != (WinterMissionState{ID: WinterMissionSnowSlave, Completions: 63, Goal: 6580, Count: 2038}) {
		t.Fatalf("kept row %+v", rows[1])
	}
	if rows[3].Goal != 5 || rows[3].Count != 0 {
		t.Fatalf("damaged row %+v", rows[3])
	}
}

/*
================
TestWinterMissionTestGoals

The port-only switch fixes the named missions' goals, a kept row's
included, and keeps them across completions; the others stay native. A
malformed value or an unknown mission refuses.
================
*/
func TestWinterMissionTestGoals(t *testing.T) {
	goals, err := ParseWinterMissionGoals("30001=3, 30002=5")
	if err != nil {
		t.Fatal(err)
	}
	kept := []WinterMissionState{{ID: WinterMissionSnowSlave, Completions: 1, Goal: 6580, Count: 4}}
	m := NewWinterMissions(kept, 10, goals)
	if rows := m.Snapshot(); rows[0].Goal != 3 || rows[1].Goal != 5 || rows[3].Goal != 5 {
		t.Fatalf("goals %+v", rows)
	}
	if got := m.Record(WinterTriggerKill, 1, "a", 10); len(got) != 2 || m.Snapshot()[1].Goal != 5 {
		t.Fatalf("completion %+v, row %+v", got, m.Snapshot()[1])
	}
	for _, bad := range []string{"30001", "30001=0", "29999=3", "30001=x"} {
		if _, err := ParseWinterMissionGoals(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
