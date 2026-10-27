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
// Version 18 adds the optional ended-quest list (endedQuestIds, native quest
// state 5: the KT blacksmith fork and QNO_CA_THIEF_5's reply). It shipped
// first in a schema 17 server, so a schema 17 authority may already carry it;
// the offline upgrade accepts those records as they are and rewrites none.
// Version 19 adds the optional job reward (job.reward, _CharTrijob.Reward:
// the thief's or hunter's weekly outcome) and the job ranking snapshot in
// metadata. The offline upgrade rewrites no record.
// Version 20 adds the optional permanent stall decoration (stallDecoration,
// the ITEM_MALL_BOOTH_* item a character applied). The offline upgrade
// rewrites no record.
// Version 21 adds the fortress production rows (fortress_item_forges,
// layout 8: _SiegeFortressItemForge). The offline upgrade adds the empty
// table and makes each guild master the sole commander fortress role holder
// (guild_leader_role.go), the one record it rewrites.
// Version 22 adds the community site's optional character fields (port-only):
// publicHidden, the player's privacy choice, levelReachedAt, the time each
// milestone level was first reached, and lastLoginAtMs, the latest world entry. Layout 9 adds the unique_kills
// table. The offline upgrade adds the empty table and rewrites no record.
// Version 23 adds publicGearHidden, the player's "hide my gear" choice for
// the community site (port-only); the upgrade rewrites no record.
// Version 24 adds mostWanted, a suited player's job-kill streak (port-only,
// SRO_MOST_WANTED); the upgrade rewrites no record.
// Version 25 changes no record: layout 10 adds the economy_ledger table
// (port-only, economy_ledger.go), and the schema stamp is what makes a release
// run the offline upgrade that adds it (release_state.store_upgrade_required
// compares schemas only). The upgrade rewrites no record.
// Version 26 adds the optional guild founding time (foundedAtMs, native
// _Guild.FoundationDate), stamped at creation. Guilds founded earlier keep
// none and show as unknown; the upgrade rewrites no record.
// Version 27 adds the optional training-camp honor, evaluation, graduation
// and join fields plus character campDisbandedAtMs (native _TrainingCamp /
// _TrainingCampMember / _TrainingCampSubMentorHonorPoint). The upgrade
// rewrites no record.
// Version 28 adds opt-in companion training/treeVersion to both schema-27
// histories. The preserving upgrade never rewrites a character record.
// Version 29 adds the optional instance return anchor (world.instanceReturn):
// the packed world and pose a character returns to when an instanced world
// ends. The preserving upgrade never rewrites a character record.
// Version 30 adds the optional made time of a fortress structure
// (madeAtMs), stamped when an upgrade raises it, so its build minutes hold
// across a restart. Structures raised earlier have none; the upgrade
// rewrites no record.
// Version 31 adds optional sequential pet cast capacity, charges and recharge period.
// Existing cooldown deadlines and every saved record survive unchanged.
// Version 32 adds the optional item-break notice opt-in (domain.Character
// BreakNotice, #1738); every saved record survives unchanged.
// Version 33 adds the optional name-change scroll state (pendingRenameName,
// pendingRenameQueuedMs, lastRenameMs; port-only, #1689,
// SRO_NAME_CHANGE_SCROLL) and layout 11's name_reservations table. The
// offline upgrade adds the empty table and rewrites no record.
const CurrentVersion = 33

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
	JobRankings  map[string]domain.JobRankings     `json:"jobRankings,omitempty"`
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
