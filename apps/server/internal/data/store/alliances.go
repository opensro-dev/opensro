/*
===========================================================================

alliances.go - the union door over the alliances table

A union changes when a guild joins or leaves it, so each save is its own
small transaction committed before the union lane goes on: the fortress
door's direct pattern.

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

// allianceSchema is layout 6's addition (authority_upgrade.go adds it to
// older authorities).
const allianceSchema = `
CREATE TABLE IF NOT EXISTS alliances (
  division    TEXT    NOT NULL,
  alliance_id INTEGER NOT NULL,
  record      TEXT    NOT NULL,
  PRIMARY KEY (division, alliance_id)
);
`

var errAllianceUnavailable = errors.New("alliance: authority unavailable")

/*
================
Alliances

The union door. Same lifetime contract as Guilds().
================
*/
func (s *Store) Alliances() domain.AllianceStore {
	return storeAllianceDoor{s: s}
}

type storeAllianceDoor struct{ s *Store }

/*
================
Alliances
================
*/
func (door storeAllianceDoor) Alliances(divisionID string) ([]domain.AllianceRecord, error) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	if door.s.db == nil {
		return nil, errAllianceUnavailable
	}
	rows, err := door.s.db.Query("SELECT alliance_id, record FROM alliances WHERE division = ? ORDER BY alliance_id", divisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AllianceRecord
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		record, err := decodeAllianceRecord(id, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

/*
================
checkAllianceRecord

A union is led by slot 0, takes its leading guild's id, and names at
least two distinct guilds.
================
*/
func checkAllianceRecord(record domain.AllianceRecord) error {
	if record.AllianceID <= 0 || record.Guilds[0] != record.AllianceID {
		return fmt.Errorf("alliance %d: inconsistent record", record.AllianceID)
	}
	seen := make(map[int64]bool, domain.AllianceSlots)
	for _, guildID := range record.Guilds {
		if guildID < 0 || guildID != 0 && seen[guildID] {
			return fmt.Errorf("alliance %d: inconsistent record", record.AllianceID)
		}
		if guildID != 0 {
			seen[guildID] = true
		}
	}
	if len(seen) < 2 {
		return fmt.Errorf("alliance %d: fewer than two guilds", record.AllianceID)
	}
	return nil
}

/*
================
decodeAllianceRecord

A row's JSON must name the row's own union.
================
*/
func decodeAllianceRecord(id int64, raw string) (domain.AllianceRecord, error) {
	var record domain.AllianceRecord
	if err := decodeJSONStrict([]byte(raw), &record); err != nil {
		return record, fmt.Errorf("alliance %d: %w", id, err)
	}
	if record.AllianceID != id {
		return record, fmt.Errorf("alliance %d: inconsistent record", id)
	}
	return record, checkAllianceRecord(record)
}

/*
================
validateAlliances

Every stored union decodes, and no guild sits in two unions of a division.
================
*/
func validateAlliances(db *sql.DB) error {
	rows, err := db.Query("SELECT division, alliance_id, record FROM alliances")
	if err != nil {
		return err
	}
	defer rows.Close()
	owner := make(map[string]map[int64]int64)
	for rows.Next() {
		var division, raw string
		var id int64
		if err := rows.Scan(&division, &id, &raw); err != nil {
			return err
		}
		record, err := decodeAllianceRecord(id, raw)
		if err != nil {
			return err
		}
		if owner[division] == nil {
			owner[division] = make(map[int64]int64)
		}
		for _, guildID := range record.Guilds {
			if guildID == 0 {
				continue
			}
			if other, taken := owner[division][guildID]; taken {
				return fmt.Errorf("alliance %d: guild %d already in alliance %d", id, guildID, other)
			}
			owner[division][guildID] = id
		}
	}
	return rows.Err()
}

/*
================
SaveAlliance
================
*/
func (door storeAllianceDoor) SaveAlliance(divisionID string, record domain.AllianceRecord, present bool) error {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || divisionID == "" || record.AllianceID <= 0 {
		return errAllianceUnavailable
	}
	var err error
	if present {
		if err = checkAllianceRecord(record); err != nil {
			return err
		}
		var raw []byte
		if raw, err = json.Marshal(record); err != nil {
			return err
		}
		_, err = s.db.Exec(
			"INSERT INTO alliances (division, alliance_id, record) VALUES (?, ?, ?) ON CONFLICT(division, alliance_id) DO UPDATE SET record = excluded.record",
			divisionID, record.AllianceID, string(raw))
	} else {
		_, err = s.db.Exec("DELETE FROM alliances WHERE division = ? AND alliance_id = ?", divisionID, record.AllianceID)
	}
	if err != nil {
		s.recordWriteFailureLocked("alliance", err)
	}
	return err
}
