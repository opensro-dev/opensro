package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

var errIncompatibleSchema = errors.New("authority database schema is incompatible with this binary")

func isVersionMismatch(err error) bool {
	return errors.Is(err, errIncompatibleSchema)
}

// CurrentVersion identifies the only authority-record schema this pre-release
// server accepts. Incompatible pre-release databases are discarded and
// recreated explicitly; production startup never rewrites them in place.
//
// Version 13 replaces world.dungeonMinimap's presentation prefix/label object
// with the semantic-only world.dungeonFloorIndex. The browser resolves all
// minimap presentation from its packed catalogue.
const CurrentVersion = 13

// SkillSeedFunc resolves the current racial base-skill set while preserving
// any already learned skill identifiers.
type SkillSeedFunc func(raceKey string, learned []uint32) ([]uint32, error)

// Meta carries persisted counters that are not owned by an individual
// gameplay record.
type Meta struct {
	GidCounter  uint32           `json:"gidCounter"`
	NextCharID  map[string]int64 `json:"nextCharId,omitempty"`
	NextGuildID map[string]int64 `json:"nextGuildId,omitempty"`
}

// retiredCharacterFields are v13 record keys no current field owns. Records
// written before the item-based loadout carry them, as does anything a
// rolled-back release writes; both still read as schema 13, so no version
// bump or rollback hazard is involved. Only an EMPTY value is dropped (all
// 18 live characters held [] on 2026-09-29): a populated one would be data
// this binary cannot represent, and fails the load instead.
var retiredCharacterFields = []string{"dressSetKeys", "weaponSetKeys"}

/*
================
stripRetiredCharacterFields
================
*/
func stripRetiredCharacterFields(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	stripped := false
	for _, name := range retiredCharacterFields {
		value, ok := fields[name]
		if !ok {
			continue
		}
		var values []string
		if err := json.Unmarshal(value, &values); err != nil || len(values) != 0 {
			return nil, fmt.Errorf("retired character field %q holds data: %s", name, value)
		}
		delete(fields, name)
		stripped = true
	}
	if !stripped {
		return raw, nil
	}
	return json.Marshal(fields)
}

/*
================
decodeCharacterStrict

Refuses unknown fields and ownerless records. A current-schema load must
never silently discard data on its next commit.
================
*/
func decodeCharacterStrict(raw json.RawMessage) (*domain.Character, error) {
	raw, err := stripRetiredCharacterFields(raw)
	if err != nil {
		return nil, err
	}
	character := &domain.Character{}
	if err := decodeJSONStrict(raw, character); err != nil {
		return nil, err
	}
	if strings.TrimSpace(character.AccountID) == "" {
		return nil, fmt.Errorf("character %q has no accountId; account ownership is mandatory in schema v%d", character.Name, CurrentVersion)
	}
	return character, nil
}
