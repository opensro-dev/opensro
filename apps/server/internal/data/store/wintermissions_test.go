/*
===========================================================================

wintermissions_test.go - the winter Server Missions' rows survive a
restart

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestWinterMissionsSurviveAReopen

The division's rows and the running bonus's end come back as they were
set; another division keeps none.
================
*/
func TestWinterMissionsSurviveAReopen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	want := domain.WinterMissions{
		Rows:         []domain.WinterMissionRow{{ID: 30001, Completions: 29, Goal: 604, Count: 288}, {ID: 30002, Completions: 63, Goal: 6580, Count: 2038}},
		ExpUpUntilMs: 1_700_000_000_000,
	}
	s.SetWinterMissions(testDivision, want)
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	got := reopened.WinterMissions(testDivision)
	if got.ExpUpUntilMs != want.ExpUpUntilMs || len(got.Rows) != 2 || got.Rows[0] != want.Rows[0] || got.Rows[1] != want.Rows[1] {
		t.Fatalf("after reopen: %+v", got)
	}
	if other := reopened.WinterMissions("another"); len(other.Rows) != 0 || other.ExpUpUntilMs != 0 {
		t.Fatalf("another division: %+v", other)
	}
}
