/*
===========================================================================

caravan_agents.go - bounded SQLite persistence for AI caravan memories and route heat

Port-only, not native (SRO_AI_CARAVANS). The two tables are layout 14's
additive step (layout_steps.go, schema 47): a fresh database creates them
from dbSchema, an older store gets them empty at boot inside the read
window or from the offline upgrade, and an older binary never reads them. Every write enforces
strict row-count and byte-size caps by pruning the oldest updated_at_ms rows
in the same transaction so the database cannot grow unbounded.

===========================================================================
*/
package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const caravanAgentSchema = `
CREATE TABLE IF NOT EXISTS caravan_agent_memories (
  division      TEXT    NOT NULL,
  agent_name    TEXT    NOT NULL,
  player_name   TEXT    NOT NULL,
  trust_score   INTEGER NOT NULL,
  kills         INTEGER NOT NULL,
  deaths        INTEGER NOT NULL,
  gold_stolen   INTEGER NOT NULL,
  last_region   INTEGER NOT NULL,
  last_note     TEXT    NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  PRIMARY KEY (division, agent_name, player_name)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_caravan_agent_memories_updated
  ON caravan_agent_memories (division, updated_at_ms);
CREATE TABLE IF NOT EXISTS caravan_route_heat (
  division      TEXT    NOT NULL,
  region_id     INTEGER NOT NULL,
  danger_score  INTEGER NOT NULL,
  ambushes      INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  PRIMARY KEY (division, region_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_caravan_route_heat_updated
  ON caravan_route_heat (division, updated_at_ms);
`

/*
================
LoadCaravanAgentMemories

Loads up to MaxCaravanTotalMemories relationship rows for a division, ordered
by most recently updated first.
================
*/
func (s *Store) LoadCaravanAgentMemories(division string) ([]domain.CaravanAgentMemoryRow, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return nil, nil
	}
	div := strings.TrimSpace(division)
	rows, err := db.Query(`
		SELECT division, agent_name, player_name, trust_score, kills, deaths, gold_stolen, last_region, last_note, updated_at_ms
		FROM caravan_agent_memories
		WHERE division = ?
		ORDER BY updated_at_ms DESC
		LIMIT ?
	`, div, domain.MaxCaravanTotalMemories)
	if err != nil {
		return nil, fmt.Errorf("query caravan agent memories: %w", err)
	}
	defer rows.Close()

	var result []domain.CaravanAgentMemoryRow
	for rows.Next() {
		var row domain.CaravanAgentMemoryRow
		var region int64
		if err := rows.Scan(
			&row.Division,
			&row.AgentName,
			&row.PlayerName,
			&row.TrustScore,
			&row.Kills,
			&row.Deaths,
			&row.GoldStolen,
			&region,
			&row.LastNote,
			&row.UpdatedAtMs,
		); err != nil {
			return nil, fmt.Errorf("scan caravan agent memory: %w", err)
		}
		row.TrustScore = domain.ClampCaravanTrust(row.TrustScore)
		row.LastRegion = uint16(region)
		row.LastNote = domain.TruncateCaravanNote(row.LastNote)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate caravan agent memories: %w", err)
	}
	return result, nil
}

/*
================
SaveCaravanAgentMemory

Upserts one agent-player relationship row and prunes the oldest rows when the
per-agent cap (MaxCaravanMemoriesPerAgent) or division cap
(MaxCaravanTotalMemories) is exceeded.
================
*/
func (s *Store) SaveCaravanAgentMemory(row domain.CaravanAgentMemoryRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	div := strings.TrimSpace(row.Division)
	agentName := strings.TrimSpace(row.AgentName)
	playerName := strings.TrimSpace(row.PlayerName)
	if agentName == "" || playerName == "" {
		return nil
	}
	trust := domain.ClampCaravanTrust(row.TrustScore)
	note := domain.TruncateCaravanNote(row.LastNote)
	kills := row.Kills
	if kills < 0 {
		kills = 0
	}
	deaths := row.Deaths
	if deaths < 0 {
		deaths = 0
	}
	goldStolen := row.GoldStolen
	if goldStolen < 0 {
		goldStolen = 0
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin caravan memory tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO caravan_agent_memories (
			division, agent_name, player_name, trust_score, kills, deaths, gold_stolen, last_region, last_note, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (division, agent_name, player_name) DO UPDATE SET
			trust_score   = excluded.trust_score,
			kills         = excluded.kills,
			deaths        = excluded.deaths,
			gold_stolen   = excluded.gold_stolen,
			last_region   = excluded.last_region,
			last_note     = excluded.last_note,
			updated_at_ms = excluded.updated_at_ms
	`, div, agentName, playerName, trust, kills, deaths, goldStolen, int64(row.LastRegion), note, row.UpdatedAtMs); err != nil {
		return fmt.Errorf("upsert caravan agent memory: %w", err)
	}

	// Enforce per-agent bound by deleting oldest rows beyond MaxCaravanMemoriesPerAgent.
	if _, err := tx.Exec(`
		DELETE FROM caravan_agent_memories
		WHERE division = ? AND agent_name = ? AND player_name NOT IN (
			SELECT player_name FROM caravan_agent_memories
			WHERE division = ? AND agent_name = ?
			ORDER BY updated_at_ms DESC, player_name ASC
			LIMIT ?
		)
	`, div, agentName, div, agentName, domain.MaxCaravanMemoriesPerAgent); err != nil {
		return fmt.Errorf("prune per-agent caravan memories: %w", err)
	}

	// Enforce division-wide bound by deleting oldest rows beyond MaxCaravanTotalMemories.
	if _, err := tx.Exec(`
		DELETE FROM caravan_agent_memories
		WHERE division = ? AND (agent_name, player_name) NOT IN (
			SELECT agent_name, player_name FROM caravan_agent_memories
			WHERE division = ?
			ORDER BY updated_at_ms DESC, agent_name ASC, player_name ASC
			LIMIT ?
		)
	`, div, div, domain.MaxCaravanTotalMemories); err != nil {
		return fmt.Errorf("prune total caravan memories: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit caravan memory tx: %w", err)
	}
	return nil
}

/*
================
LoadCaravanRouteHeat

Loads up to MaxCaravanRouteHeatRegions per-region danger rows for a division.
================
*/
func (s *Store) LoadCaravanRouteHeat(division string) ([]domain.CaravanRouteHeatRow, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return nil, nil
	}
	div := strings.TrimSpace(division)
	rows, err := db.Query(`
		SELECT division, region_id, danger_score, ambushes, updated_at_ms
		FROM caravan_route_heat
		WHERE division = ?
		ORDER BY updated_at_ms DESC
		LIMIT ?
	`, div, domain.MaxCaravanRouteHeatRegions)
	if err != nil {
		return nil, fmt.Errorf("query caravan route heat: %w", err)
	}
	defer rows.Close()

	var result []domain.CaravanRouteHeatRow
	for rows.Next() {
		var row domain.CaravanRouteHeatRow
		var region int64
		if err := rows.Scan(&row.Division, &region, &row.DangerScore, &row.Ambushes, &row.UpdatedAtMs); err != nil {
			return nil, fmt.Errorf("scan caravan route heat: %w", err)
		}
		row.RegionID = uint16(region)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate caravan route heat: %w", err)
	}
	return result, nil
}

/*
================
SaveCaravanRouteHeat

Upserts one route heat row and prunes the oldest regions when the division
exceeds MaxCaravanRouteHeatRegions.
================
*/
func (s *Store) SaveCaravanRouteHeat(row domain.CaravanRouteHeatRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	div := strings.TrimSpace(row.Division)
	danger := row.DangerScore
	if danger < 0 {
		danger = 0
	}
	if danger > 1000 {
		danger = 1000
	}
	ambushes := row.Ambushes
	if ambushes < 0 {
		ambushes = 0
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin caravan route heat tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO caravan_route_heat (division, region_id, danger_score, ambushes, updated_at_ms)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (division, region_id) DO UPDATE SET
			danger_score  = excluded.danger_score,
			ambushes      = excluded.ambushes,
			updated_at_ms = excluded.updated_at_ms
	`, div, int64(row.RegionID), danger, ambushes, row.UpdatedAtMs); err != nil {
		return fmt.Errorf("upsert caravan route heat: %w", err)
	}

	if _, err := tx.Exec(`
		DELETE FROM caravan_route_heat
		WHERE division = ? AND region_id NOT IN (
			SELECT region_id FROM caravan_route_heat
			WHERE division = ?
			ORDER BY updated_at_ms DESC, region_id ASC
			LIMIT ?
		)
	`, div, div, domain.MaxCaravanRouteHeatRegions); err != nil {
		return fmt.Errorf("prune caravan route heat: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit caravan route heat tx: %w", err)
	}
	return nil
}
