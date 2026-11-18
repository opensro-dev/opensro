/*
===========================================================================

caravan_hires.go - the AI caravan hires in flight (port-only, SRO_AI_CARAVANS)

At most one row per hired agent, deleted when the hire ends, so the table
stays as small as the roster. Created at open like the referral tables:
additive, so no stamp moves and an older binary on the same file ignores
it (its hires were never persisted anyway).

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const caravanHireSchema = `
CREATE TABLE IF NOT EXISTS caravan_hires (
  division        TEXT    NOT NULL,
  agent_name      TEXT    NOT NULL,
  player_name     TEXT    NOT NULL,
  hired_at_ms     INTEGER NOT NULL,
  hire_town       TEXT    NOT NULL,
  goods_cost_gold INTEGER NOT NULL,
  cargo_stacks    INTEGER NOT NULL,
  PRIMARY KEY (division, agent_name)
) WITHOUT ROWID;
`

// maxCaravanHires bounds a load; the roster holds 40 agents.
const maxCaravanHires = 256

func ensureCaravanHires(db *sql.DB) error {
	if _, err := db.Exec(caravanHireSchema); err != nil {
		return fmt.Errorf("caravan hires: %w", err)
	}
	return nil
}

/*
================
LoadCaravanHires
================
*/
func (s *Store) LoadCaravanHires(division string) ([]domain.CaravanHireRow, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT division, agent_name, player_name, hired_at_ms, hire_town, goods_cost_gold, cargo_stacks
		FROM caravan_hires WHERE division = ? ORDER BY hired_at_ms LIMIT ?
	`, strings.TrimSpace(division), maxCaravanHires)
	if err != nil {
		return nil, fmt.Errorf("query caravan hires: %w", err)
	}
	defer rows.Close()
	var out []domain.CaravanHireRow
	for rows.Next() {
		var row domain.CaravanHireRow
		if err := rows.Scan(&row.Division, &row.AgentName, &row.PlayerName, &row.HiredAtMs, &row.HireTown, &row.GoodsCostGold, &row.CargoStacks); err != nil {
			return nil, fmt.Errorf("scan caravan hire: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

/*
================
SaveCaravanHire / DeleteCaravanHire
================
*/
func (s *Store) SaveCaravanHire(row domain.CaravanHireRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || strings.TrimSpace(row.AgentName) == "" || strings.TrimSpace(row.PlayerName) == "" {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO caravan_hires (division, agent_name, player_name, hired_at_ms, hire_town, goods_cost_gold, cargo_stacks)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (division, agent_name) DO UPDATE SET
			player_name = excluded.player_name, hired_at_ms = excluded.hired_at_ms,
			hire_town = excluded.hire_town, goods_cost_gold = excluded.goods_cost_gold,
			cargo_stacks = excluded.cargo_stacks
	`, strings.TrimSpace(row.Division), strings.TrimSpace(row.AgentName), strings.TrimSpace(row.PlayerName),
		row.HiredAtMs, row.HireTown, max(row.GoodsCostGold, 0), max(row.CargoStacks, 0))
	if err != nil {
		return fmt.Errorf("upsert caravan hire: %w", err)
	}
	return nil
}

func (s *Store) DeleteCaravanHire(division, agentName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM caravan_hires WHERE division = ? AND agent_name = ?`,
		strings.TrimSpace(division), strings.TrimSpace(agentName)); err != nil {
		return fmt.Errorf("delete caravan hire: %w", err)
	}
	return nil
}
