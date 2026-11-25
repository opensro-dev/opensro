/*
===========================================================================

caravan_escort_days_test.go - the daily escort-cap ledger persists

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCaravanEscortDaysPersistAcrossReopen

A Hunter's paid-escort count for a day survives a store re-open (the
restart cannot reset the daily cap), days and divisions stay apart,
and an upsert overwrites the count rather than adding a row.
================
*/
func TestCaravanEscortDaysPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	row := domain.CaravanEscortDayRow{Division: "global-official", HunterName: "IronGuard", Day: "2026-11-26", PaidCount: 2}
	if err := s.SaveCaravanEscortDay(row); err != nil {
		t.Fatalf("save escort day: %v", err)
	}
	row.PaidCount = 3
	if err := s.SaveCaravanEscortDay(row); err != nil {
		t.Fatalf("upsert escort day: %v", err)
	}
	if err := s.SaveCaravanEscortDay(domain.CaravanEscortDayRow{
		Division: "global-official", HunterName: "IronGuard", Day: "2026-11-27", PaidCount: 1,
	}); err != nil {
		t.Fatalf("save next day: %v", err)
	}
	s.Close()

	s, err = Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	rows, err := s.LoadCaravanEscortDays("global-official")
	if err != nil {
		t.Fatalf("load escort days: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("loaded %d escort-day rows, want 2: %+v", len(rows), rows)
	}
	byDay := make(map[string]int)
	for _, loaded := range rows {
		byDay[loaded.Day] = loaded.PaidCount
	}
	if byDay["2026-11-26"] != 3 || byDay["2026-11-27"] != 1 {
		t.Fatalf("escort-day counts after reopen: %v", byDay)
	}
	if rows, err := s.LoadCaravanEscortDays("elsewhere"); err != nil || len(rows) != 0 {
		t.Fatalf("another division loaded %d rows, err %v", len(rows), err)
	}
}
