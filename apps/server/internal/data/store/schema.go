/*
===========================================================================

schema.go - strict authority record versions and character decoding

A version identifies the retained value graph, not just SQL tables. Older
binaries must refuse new record fields instead of losing them on a write:
the version check refuses a newer store at boot, and a record's unknown
top-level fields survive the next write regardless (json.go, #3090).

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
// Version 34 adds the optional Halloween daily box count (halloweenBoxDay,
// halloweenBoxCount; port-only, SRO_HALLOWEEN_EVENT). The offline upgrade
// rewrites no record.
// Version 35 adds the optional wanted-level lapse (mostWanted.expiresAtMs,
// port-only, #1849): the time a suited player's wanted level lapses.
// Streaks saved earlier carry none and lapse at their next level. The
// offline upgrade rewrites no record.
// Version 36 moves the Halloween daily box count to the account (the
// halloweenBoxes metadata plane, port-only, #2547): the cap counts per
// account, not per character. The character halloweenBoxDay/Count fields
// stay as the seed the account record is derived from at first load; the
// offline upgrade rewrites no record.
// Version 37 adds the optional time-online record (character online,
// port-only, #2675): the credited total and rolling UTC week the public
// boards rank by. Characters saved earlier carry none and start
// counting at their next login; the offline upgrade rewrites no record.
// Version 38 adds the Academy cumulated-buff state to camp members
// (buffValue, buffWindowStartMs, buffLive, reservedBuffs; #2270): the
// saved buff a student accumulates and the received rows it passes to
// the Guardian. Members saved earlier carry none of it and hold no
// buff; the offline upgrade rewrites no record.
// Version 39 changes no record: layout 12 adds the silk_history table
// (port-only, silkhistory.go), the movement record the Item Mall's silk
// history window lists (#2269). The upgrade rewrites no record.
// Version 40 replaces the wanted level's seven-day lapse with a per-day
// decay (mostWanted.nextDecayAtMs, port-only, #2721): every counted kill
// restamps it, and each day without one drops the level by one. The
// schema 35 expiresAtMs stays in the record as an unread seed, like the
// schema 36 Halloween counts. Streaks saved earlier carry no decay stamp
// and are stamped fresh at their first judging door, so no saved streak
// decays early. It also adds the optional forge-bound mark on item rows
// (forgeBound, port-only, #1980): an item a GM forged with /FORGE can
// never be traded, sold or handed to another character; rows saved
// earlier carry no mark and are unbound. The offline upgrade rewrites
// no record.
// Version 41 adds the Halloween Gourd box count to the account record
// (halloweenBoxes gourdCount, port-only, #2950): Gourd-kill boxes mint
// against their own daily cap, so ordinary grinding no longer starves
// a wave. Rows saved earlier carry no Gourd count and start at zero;
// the offline upgrade rewrites no record.
// Version 42 adds optional arena entry markers and its durable reward chest.
// Version 43 moves the survival arena's daily entry count to the account
// (the sookArenaAccounts metadata plane, port-only, #3322): the three
// entries a UTC day count per account, not per character. The character
// sookArenaDay/sookArenaEntries fields stay as the seed the account
// record is derived from at first load; the offline upgrade rewrites no
// record.
const CurrentVersion = 43

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
	// HalloweenBoxes is the per-account daily box count (#2547), keyed
	// by account id. Records are pointers: a settlement mutates the one
	// record inside its door and the commit stages it, exactly like a
	// character record.
	HalloweenBoxes map[string]*domain.HalloweenBoxAccount `json:"halloweenBoxes,omitempty"`
	// SookArenaAccounts is the per-account survival arena daily entry
	// count (#3322), keyed by account id, with HalloweenBoxes' pointer
	// discipline: the entry mutates the one record inside its door.
	SookArenaAccounts map[string]*domain.SookArenaAccount `json:"sookArenaAccounts,omitempty"`
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

Refuses ownerless records and unknown fields below the top level. The
record's unknown top-level fields are returned for the store to write
back (json.go, #3090): a current-schema load must never silently
discard data on its next commit.
================
*/
func decodeCharacterStrict(raw json.RawMessage) (*domain.Character, unknownFields, error) {
	raw, err := stripRetiredCharacterFields(raw)
	if err != nil {
		return nil, "", err
	}
	character := &domain.Character{}
	unknown, err := decodeRecordFields(raw, character)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(character.AccountID) == "" {
		return nil, "", fmt.Errorf("character %q has no accountId; account ownership is mandatory in schema v%d", character.Name, CurrentVersion)
	}
	return character, unknown, nil
}
