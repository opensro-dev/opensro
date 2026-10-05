/*
===========================================================================

fortresses.go - the fortress door over the fortresses and fortress_requests tables

A fortress changes hands a few times a week and a request a few times a
war, so each save is its own small transaction committed before the war
lane goes on: the itemmall door's direct pattern, not the dirty-commit
planes.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"opensro.online/server/internal/domain"
)

// fortressSchema is layout 6's addition (authority_upgrade.go adds it to
// older authorities).
const fortressSchema = `
CREATE TABLE IF NOT EXISTS fortresses (
  division    TEXT    NOT NULL,
  fortress_id INTEGER NOT NULL,
  record      TEXT    NOT NULL,
  PRIMARY KEY (division, fortress_id)
);
CREATE TABLE IF NOT EXISTS fortress_structures (
  division        TEXT    NOT NULL,
  fortress_id     INTEGER NOT NULL,
  event_struct_id INTEGER NOT NULL,
  record          TEXT    NOT NULL,
  PRIMARY KEY (division, fortress_id, event_struct_id)
);
CREATE TABLE IF NOT EXISTS fortress_requests (
  division     TEXT    NOT NULL,
  fortress_id  INTEGER NOT NULL,
  guild_id     INTEGER NOT NULL,
  request_type INTEGER NOT NULL CHECK (request_type BETWEEN 0 AND 1),
  PRIMARY KEY (division, fortress_id, guild_id)
);
`

var errFortressUnavailable = errors.New("fortress: authority unavailable")

/*
================
Fortresses

The fortress door. Same lifetime contract as Guilds().
================
*/
func (s *Store) Fortresses() domain.FortressStore {
	return storeFortressDoor{s: s}
}

type storeFortressDoor struct{ s *Store }

/*
================
FortressState
================
*/
func (door storeFortressDoor) FortressState(divisionID string) ([]domain.FortressRecord, []domain.FortressRequestRecord, error) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	if door.s.db == nil {
		return nil, nil, errFortressUnavailable
	}
	rows, err := door.s.db.Query("SELECT fortress_id, record FROM fortresses WHERE division = ? ORDER BY fortress_id", divisionID)
	if err != nil {
		return nil, nil, err
	}
	var records []domain.FortressRecord
	for rows.Next() {
		var id uint32
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		record, err := decodeFortressRecord(id, raw)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		records = append(records, record)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	requests, err := door.s.db.Query("SELECT fortress_id, guild_id, request_type FROM fortress_requests WHERE division = ? ORDER BY fortress_id, guild_id", divisionID)
	if err != nil {
		return nil, nil, err
	}
	defer requests.Close()
	var out []domain.FortressRequestRecord
	for requests.Next() {
		var request domain.FortressRequestRecord
		if err := requests.Scan(&request.FortressID, &request.GuildID, &request.Kind); err != nil {
			return nil, nil, err
		}
		out = append(out, request)
	}
	return records, out, requests.Err()
}

/*
================
decodeFortressRecord

A row's JSON must name the row's own fortress.
================
*/
func decodeFortressRecord(id uint32, raw string) (domain.FortressRecord, error) {
	var record domain.FortressRecord
	if err := decodeJSONStrict([]byte(raw), &record); err != nil {
		return record, fmt.Errorf("fortress %d: %w", id, err)
	}
	if record.FortressID != id || record.GuildID < 0 || record.TempGuildID < 0 {
		return record, fmt.Errorf("fortress %d: inconsistent record", id)
	}
	return record, nil
}

/*
================
validateFortresses

Every stored fortress and structure row decodes and names its own keys.
================
*/
func validateFortresses(db *sql.DB) error {
	rows, err := db.Query("SELECT fortress_id, record FROM fortresses")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uint32
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		if _, err := decodeFortressRecord(id, raw); err != nil {
			rows.Close()
			return err
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	structures, err := db.Query("SELECT fortress_id, event_struct_id, record FROM fortress_structures")
	if err != nil {
		return err
	}
	defer structures.Close()
	for structures.Next() {
		var fortressID, zone uint32
		var raw string
		if err := structures.Scan(&fortressID, &zone, &raw); err != nil {
			return err
		}
		if _, err := decodeFortressStructure(fortressID, zone, raw); err != nil {
			return err
		}
	}
	return structures.Err()
}

/*
================
SaveFortress
================
*/
func (door storeFortressDoor) SaveFortress(divisionID string, record domain.FortressRecord) error {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || divisionID == "" || record.FortressID == 0 {
		return errFortressUnavailable
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO fortresses (division, fortress_id, record) VALUES (?, ?, ?) ON CONFLICT(division, fortress_id) DO UPDATE SET record = excluded.record",
		divisionID, record.FortressID, string(raw))
	if err != nil {
		s.recordWriteFailureLocked("fortress", err)
	}
	return err
}

/*
================
SaveFortressRequest
================
*/
func (door storeFortressDoor) SaveFortressRequest(divisionID string, request domain.FortressRequestRecord, present bool) error {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || divisionID == "" || request.FortressID == 0 || request.GuildID <= 0 || request.Kind > 1 {
		return errFortressUnavailable
	}
	var err error
	if present {
		_, err = s.db.Exec(
			"INSERT INTO fortress_requests (division, fortress_id, guild_id, request_type) VALUES (?, ?, ?, ?) ON CONFLICT(division, fortress_id, guild_id) DO UPDATE SET request_type = excluded.request_type",
			divisionID, request.FortressID, request.GuildID, request.Kind)
	} else {
		_, err = s.db.Exec("DELETE FROM fortress_requests WHERE division = ? AND fortress_id = ? AND guild_id = ?",
			divisionID, request.FortressID, request.GuildID)
	}
	if err != nil {
		s.recordWriteFailureLocked("fortress-request", err)
	}
	return err
}

/*
================
FortressStructures
================
*/
func (door storeFortressDoor) FortressStructures(divisionID string) ([]domain.FortressStructureRecord, error) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	if door.s.db == nil {
		return nil, errFortressUnavailable
	}
	rows, err := door.s.db.Query("SELECT fortress_id, event_struct_id, record FROM fortress_structures WHERE division = ? ORDER BY fortress_id, event_struct_id", divisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FortressStructureRecord
	for rows.Next() {
		var fortressID, zone uint32
		var raw string
		if err := rows.Scan(&fortressID, &zone, &raw); err != nil {
			return nil, err
		}
		structure, err := decodeFortressStructure(fortressID, zone, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, structure)
	}
	return out, rows.Err()
}

/*
================
decodeFortressStructure

A row's JSON must name the row's own fortress and event zone.
================
*/
func decodeFortressStructure(fortressID, zone uint32, raw string) (domain.FortressStructureRecord, error) {
	var structure domain.FortressStructureRecord
	if err := decodeJSONStrict([]byte(raw), &structure); err != nil {
		return structure, fmt.Errorf("fortress %d structure %d: %w", fortressID, zone, err)
	}
	if structure.FortressID != fortressID || structure.EventStructID != zone || structure.RefObjID == 0 || structure.OwnerGuildID < 0 {
		return structure, fmt.Errorf("fortress %d structure %d: inconsistent record", fortressID, zone)
	}
	return structure, nil
}

/*
================
SaveFortressStructure
================
*/
func (door storeFortressDoor) SaveFortressStructure(divisionID string, structure domain.FortressStructureRecord, present bool) error {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || divisionID == "" || structure.FortressID == 0 || structure.EventStructID == 0 {
		return errFortressUnavailable
	}
	var err error
	if present {
		if structure.RefObjID == 0 || structure.OwnerGuildID < 0 {
			return fmt.Errorf("fortress %d structure %d: inconsistent record", structure.FortressID, structure.EventStructID)
		}
		var raw []byte
		if raw, err = json.Marshal(structure); err != nil {
			return err
		}
		_, err = s.db.Exec(
			"INSERT INTO fortress_structures (division, fortress_id, event_struct_id, record) VALUES (?, ?, ?, ?) ON CONFLICT(division, fortress_id, event_struct_id) DO UPDATE SET record = excluded.record",
			divisionID, structure.FortressID, structure.EventStructID, string(raw))
	} else {
		_, err = s.db.Exec("DELETE FROM fortress_structures WHERE division = ? AND fortress_id = ? AND event_struct_id = ?",
			divisionID, structure.FortressID, structure.EventStructID)
	}
	if err != nil {
		s.recordWriteFailureLocked("fortress-structure", err)
	}
	return err
}
