/*
===========================================================================

economy_reattribute_test.go - unattributed gold takes its door's reason

The live shard's So-Ok arena payouts were recorded as "unattributed"
before the reason table named their door. These tests drive the open
pass over folded and unfolded rows and read the buckets the console
reads.

===========================================================================
*/

package store

import "testing"

/*
================
seedDoorRow

One unattributed ledger row written through door.
================
*/
func seedDoorRow(t *testing.T, s *Store, atMs int64, door string, gold int64) {
	t.Helper()
	if _, err := s.syncDBLocked().Exec(`INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance)
VALUES (?, ?, 7, 'unattributed', ?, ?, 0)`, atMs, testDivision, door, gold); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestReattributeMovesFoldedArenaGold

Folded arena rows leave the unattributed bucket for arena_reward; a door
the table still does not know stays unattributed; a row the fold has not
reached yet is renamed and folds under its new reason.
================
*/
func TestReattributeMovesFoldedArenaGold(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	seedDoorRow(t, s, foldHour1+60_000, "sook-payout", 1_000_000)
	seedDoorRow(t, s, foldHour1+90_000, "sook-payout", 500_000)
	seedDoorRow(t, s, foldHour1+95_000, "no-such-door", 7)
	lane := testFoldLane(s)
	if folded, err := lane.foldNewRows(); err != nil || !folded {
		t.Fatalf("fold %v, %v", folded, err)
	}
	seedDoorRow(t, s, foldHour2+60_000, "sook-recovered-payout", 30)

	changed, err := reattributeLedger(s.syncDBLocked())
	if err != nil {
		t.Fatal(err)
	}
	if changed != 3 {
		t.Fatalf("changed %d rows, want 3", changed)
	}
	hours := ledgerHourTotals(t, s)
	if hours["arena_reward gain"] != 1_500_000 || hours["unattributed gain"] != 7 {
		t.Fatalf("buckets after the pass %v", hours)
	}
	if _, err := lane.foldNewRows(); err != nil {
		t.Fatal(err)
	}
	if hours := ledgerHourTotals(t, s); hours["arena_reward gain"] != 1_500_030 {
		t.Fatalf("buckets after the next fold %v", hours)
	}
	for _, row := range ledgerRows(t, s) {
		if row.door != "no-such-door" && row.reason != "arena_reward" {
			t.Fatalf("row kept its old reason: %+v", row)
		}
	}
	// A second open finds nothing left to move.
	if again, err := reattributeLedger(s.syncDBLocked()); err != nil || again != 0 {
		t.Fatalf("second pass %d, %v", again, err)
	}
}
