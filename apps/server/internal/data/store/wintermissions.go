/*
===========================================================================

wintermissions.go - the winter Server Missions' rows, per division

The v1.188 shard kept the missions in _ServerEvent and the running EXP
bonus beside them; this plane keeps the same per division in metadata, so
a restart resumes a half-filled mission instead of starting it over. The
plane is rewritten whole when the game sets it (SetWinterMissions).

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const metaKeyWinterMissions = "winterMissions"

/*
================
Store.WinterMissions

The division's kept rows; the zero value when none were kept.
================
*/
func (s *Store) WinterMissions(division string) domain.WinterMissions {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.meta.WinterMissions[division]
	kept.Rows = append([]domain.WinterMissionRow(nil), kept.Rows...)
	return kept
}

/*
================
Store.SetWinterMissions

Replaces the division's rows and commits them. The game throttles its
calls (action/winter_missions.go), so a hunt does not commit per kill.
================
*/
func (s *Store) SetWinterMissions(division string, missions domain.WinterMissions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta.WinterMissions == nil {
		s.meta.WinterMissions = map[string]domain.WinterMissions{}
	}
	missions.Rows = append([]domain.WinterMissionRow(nil), missions.Rows...)
	s.meta.WinterMissions[division] = missions
	s.changes.winterMissions = true
	s.commitLocked("winter-missions " + division)
}

/*
================
loadWinterMissions

An authority that never ran the missions has no row. A division's unknown
top-level fields are returned for the next write (json.go).
================
*/
func loadWinterMissions(db *sql.DB) (map[string]domain.WinterMissions, map[string]unknownFields, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyWinterMissions).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]domain.WinterMissions{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var missions map[string]domain.WinterMissions
	unknown, err := decodePlaneFields([]byte(raw), &missions)
	if err != nil {
		return nil, nil, fmt.Errorf("winter missions: %w", err)
	}
	for division, kept := range missions {
		if division == "" || strings.TrimSpace(division) != division || !kept.Valid() {
			return nil, nil, fmt.Errorf("invalid winter missions for division %q", division)
		}
	}
	return missions, unknown, nil
}

/*
================
winterMissionsRecord

The meta row's bytes; empty when no division ran the missions, in which
case no writer puts a row down.
================
*/
func winterMissionsRecord(missions map[string]domain.WinterMissions, unknown map[string]unknownFields) (string, error) {
	if len(missions) == 0 {
		return "", nil
	}
	raw, err := encodePlaneFields(missions, unknown)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

/*
================
writeWinterMissions
================
*/
func writeWinterMissions(tx *sql.Tx, missions map[string]domain.WinterMissions) error {
	record, err := winterMissionsRecord(missions, nil)
	if err != nil || record == "" {
		return err
	}
	return upsertMetaTx(tx, metaKeyWinterMissions, record)
}
