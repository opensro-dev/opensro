/*
===========================================================================

caravan_escort_days.go - the AI caravan daily escort-reward ledger (slice 5)

Port-only, not native (SRO_AI_CARAVANS). caravan_escort_days is layout
17's additive step (layout_steps.go, schema 60): one row per Hunter per
UTC day counting the AI-trader escort rewards already paid, so the daily
cap (caravanai.MaxPaidEscortsPerDay) survives a restart. Rows are never
pruned here; the table grows by at most one row per Hunter per day.

===========================================================================
*/
package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const caravanEscortDaySchema = `
CREATE TABLE IF NOT EXISTS caravan_escort_days (
  division   TEXT    NOT NULL,
  hunter     TEXT    NOT NULL,
  day        TEXT    NOT NULL,
  paid_count INTEGER NOT NULL,
  PRIMARY KEY (division, hunter, day)
) WITHOUT ROWID;
`

/*
================
LoadCaravanEscortDays

Loads every paid-escort day row for a division (one row per Hunter per
day; bounded by the roster of Hunters who ever took a paid escort).
================
*/
func (s *Store) LoadCaravanEscortDays(division string) ([]domain.CaravanEscortDayRow, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT division, hunter, day, paid_count
		FROM caravan_escort_days
		WHERE division = ?
	`, strings.TrimSpace(division))
	if err != nil {
		return nil, fmt.Errorf("query caravan escort days: %w", err)
	}
	defer rows.Close()

	var result []domain.CaravanEscortDayRow
	for rows.Next() {
		var row domain.CaravanEscortDayRow
		if err := rows.Scan(&row.Division, &row.HunterName, &row.Day, &row.PaidCount); err != nil {
			return nil, fmt.Errorf("scan caravan escort day: %w", err)
		}
		if row.PaidCount < 0 {
			row.PaidCount = 0
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate caravan escort days: %w", err)
	}
	return result, nil
}

/*
================
SaveCaravanEscortDay

Upserts one Hunter's paid-escort count for one day.
================
*/
func (s *Store) SaveCaravanEscortDay(row domain.CaravanEscortDayRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	div := strings.TrimSpace(row.Division)
	hunter := strings.TrimSpace(row.HunterName)
	day := strings.TrimSpace(row.Day)
	if hunter == "" || day == "" {
		return nil
	}
	paid := row.PaidCount
	if paid < 0 {
		paid = 0
	}
	if _, err := s.db.Exec(`
		INSERT INTO caravan_escort_days (division, hunter, day, paid_count)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (division, hunter, day) DO UPDATE SET
			paid_count = excluded.paid_count
	`, div, hunter, day, paid); err != nil {
		return fmt.Errorf("upsert caravan escort day: %w", err)
	}
	return nil
}
