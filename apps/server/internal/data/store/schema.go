/*
===========================================================================

schema.go - strict authority record versions and character decoding

A version identifies the retained value graph, not just SQL tables. Older
binaries must refuse new record fields instead of losing them on a write.

===========================================================================
*/
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

var errIncompatibleSchema = errors.New("authority database schema is incompatible with this binary")

/*
================
isVersionMismatch
================
*/
func isVersionMismatch(err error) bool {
	return errors.Is(err, errIncompatibleSchema)
}

// CurrentVersion identifies the only authority-record schema this pre-release
// server accepts. Supported older databases are upgraded explicitly offline;
// production startup never rewrites them in place.
//
// Version 13 replaces world.dungeonMinimap's presentation prefix/label object
// with the semantic-only world.dungeonFloorIndex. The browser resolves all
// minimap presentation from its packed catalogue.
//
// Version 14 adds optional character fields (paramJobs, itemGroupCooldowns,
// timedSkillJobs, skillActionRecoveryUntilMs) and comes with table layout 5.
// Records decode strictly, so a schema 13 server cannot read a character that
// carries them; sro-authority-upgrade converts a schema 13 authority offline.
// Version 15 retains item-owned companions in player/warehouse/ground rows,
// including independent leases and summon generations. Table layout stays 5.
// The offline upgrader preserves schema 13 and 14 records and their backups.
// Version 16 adds the optional world of the recorded recall and death points
// (world.lastRecallPoint.world, world.lastDeathPoint.world): native keeps the
// GameWorldID beside each point, and the optional fortress-return cooldown
// (fortressReturnUntilMs). Table layout 6 adds the fortress and union tables; schema 15
// records convert unchanged.
// Version 17 retains trade-cargo owner aliases and personal weekly reward
// contributions, with the shard-wide reward pools in metadata. Layout stays 6;
// the preserving offline upgrade leaves existing records unchanged.
const CurrentVersion = 17

// SkillSeedFunc resolves the current racial base-skill set while preserving
// any already learned skill identifiers.
/*
================
SkillSeedFunc
================
*/
type SkillSeedFunc func(raceKey string, learned []uint32) ([]uint32, error)

// Meta carries persisted counters that are not owned by an individual
// gameplay record.
/*
================
Meta
================
*/
type Meta struct {
	GidCounter   uint32                            `json:"gidCounter"`
	NextCharID   map[string]int64                  `json:"nextCharId,omitempty"`
	NextGuildID  map[string]int64                  `json:"nextGuildId,omitempty"`
	TradeRewards map[string]domain.TradeRewardPool `json:"tradeRewards,omitempty"`
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
