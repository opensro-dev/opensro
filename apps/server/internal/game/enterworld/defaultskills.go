package enterworld

import (
	"fmt"
	"reflect"
)

// The character-creation skill seed: the racial base attacks retail grants
// at creation.
//
// MECHANISM (lane6 notes section 3, adopt-wave): retail seeds a fresh
// character's learned-skill list in the _AddNewChar stored procedure from
// the _RefCharDefault_Skill table (vendor SQL, Korean comments intact),
// selecting rows WHERE Race = the character's country OR Race = 3 (both).
// The v1.150 client never self-grants and tolerates an empty list (lane4
// notes), but retail characters never HAD an empty list - auto-attack
// availability on a fresh retail character never depended on a manual
// 0-SP learn.
//
// THE LISTS ARE CODENAMES, NEVER NUMERIC IDS - this is load-bearing, not
// style. The published 1.188 _RefCharDefault_Skill ids resolve in OUR
// v1.150 skilldata to chain SUB-rows (8419/8420 - rows the learn plane
// refuses by design), wrong-kind rows, TSKILL_* test rows, and one id
// that does not exist at all (lane6 3.2, the version-contamination
// catch). The EU id space shifted between 1.150 and 1.188; codenames are
// the version-stable key. The 13 rows below are the v1.150-native
// equivalent set (lane6 3.3): every non-TSKILL *_BASE_01 weapon base plus
// SKILL_PUNCH_01, verified against the shipped table to be level 1 /
// SP 0 / single-row groups that no chain link targets.
//
// Resolution happens at load time against the SHIPPED skilldata and FAILS
// LOUD when a codename does not resolve or resolves to a row that is not
// a level-1 / 0-SP ladder root: a refusal is recoverable, a silently
// short (or garbage) seed persisted into character records is not.

// chDefaultSkillCodenames is the Chinese creation seed: punch plus the
// three CH weapon bases (1.188 Race-0 rows + the shared Race-3 punch).
var chDefaultSkillCodenames = []string{
	"SKILL_PUNCH_01",
	"SKILL_CH_SWORD_BASE_01",
	"SKILL_CH_SPEAR_BASE_01",
	"SKILL_CH_BOW_BASE_01",
}

// euDefaultSkillCodenames is the European creation seed: the nine EU
// weapon bases (1.188 Race-1 rows), plus SKILL_PUNCH_01.
//
// Rechecked against the restored SQL data page @0x13a200: slot 96 has
// fixed payload Race=3, SkillID=1. The creation query includes Race=3 for
// both countries. Punch is therefore shared, not an inferred EU convenience.
// Table identity comes from the sysschobjs row in the restored SQL backup, object
// 1430152736. See the gameplay work item's read-only extraction evidence.
var euDefaultSkillCodenames = []string{
	"SKILL_PUNCH_01",
	"SKILL_EU_SWORD_BASE_01",
	"SKILL_EU_TSWORD_BASE_01",
	"SKILL_EU_AXE_BASE_01",
	"SKILL_EU_CROSSBOW_BASE_01",
	"SKILL_EU_DAGGER_BASE_01",
	"SKILL_EU_STAFF_BASE_01",
	"SKILL_EU_WAND_WARLOCK_BASE_01",
	"SKILL_EU_HARP_BASE_01",
	"SKILL_EU_WAND_CLERIC_BASE_01",
}

// DefaultSkillCodenames answers the racial creation-seed codenames
// (defensive copy; race resolution mirrors DefaultMasteries).
func DefaultSkillCodenames(raceKey string) []string {
	codenames := euDefaultSkillCodenames
	if raceKey == RaceKeyChina {
		codenames = chDefaultSkillCodenames
	}
	out := make([]string, len(codenames))
	copy(out, codenames)
	return out
}

// SkillCodenameSource resolves skilldata rows by their version-stable
// codename. TextdataSkills implements it; it is deliberately separate
// from SkillDataSource so the learn plane's test fakes stay untouched.
type SkillCodenameSource interface {
	SkillByCodename(codename string) (SkillRow, bool)
}

// DefaultSkillRows resolves the racial creation seed against the shipped
// skilldata. FAIL LOUD contract: any codename that does not resolve, or
// resolves to a row that is not a level-1 / 0-SP ladder root, errors with
// the codename named - the caller must refuse rather than persist a short
// or wrong seed.
func DefaultSkillRows(source SkillCodenameSource, raceKey string) ([]SkillRow, error) {
	if isNilSkillCodenameSource(source) {
		return nil, fmt.Errorf("default skill seed: no skilldata source")
	}
	codenames := DefaultSkillCodenames(raceKey)
	rows := make([]SkillRow, 0, len(codenames))
	for _, codename := range codenames {
		row, ok := source.SkillByCodename(codename)
		if !ok {
			return nil, fmt.Errorf("default skill seed: codename %s does not resolve in the shipped skilldata (missing or renamed row - refusing to seed a short list)", codename)
		}
		// The seed's pinned shape: ladder roots at level 1 costing 0 SP
		// (verified over the shipped table). A violation means the
		// codename now names a different KIND of row - exactly the
		// version-drift failure the codename indirection exists to catch.
		if row.Level != 1 || row.SPCost != 0 || row.ChainSub {
			return nil, fmt.Errorf("default skill seed: codename %s resolves to id %d with level %d / SP %d / chainSub %v - not a level-1 0-SP ladder root, refusing to seed it", codename, row.ID, row.Level, row.SPCost, row.ChainSub)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func isNilSkillCodenameSource(source SkillCodenameSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

// DefaultSkillSeeder adapts a skilldata source into the store's creation/
// backfill hook: given a race and the already-learned ids, it answers the
// racial base-attack ids MISSING from that list, in seed order.
//
// Idempotence is by id, which the shipped data makes exactly equivalent
// to by-group: every base row is the ONLY row of its group (single-level
// groups, verified over all shards), so a character can never hold a
// "higher level" of a base that an id comparison would miss. A character
// who already 0-SP-learned some bases gets only the remainder; a fresh
// list gets the full racial seed.
//
// The source must support codename lookup (TextdataSkills does); a
// source without it fails every call loud rather than seeding nothing.
func DefaultSkillSeeder(source SkillDataSource) func(raceKey string, learned []uint32) ([]uint32, error) {
	codenameSource, _ := source.(SkillCodenameSource)
	return func(raceKey string, learned []uint32) ([]uint32, error) {
		if isNilSkillCodenameSource(codenameSource) {
			return nil, fmt.Errorf("default skill seed: skilldata source cannot resolve codenames")
		}
		rows, err := DefaultSkillRows(codenameSource, raceKey)
		if err != nil {
			return nil, err
		}
		have := make(map[uint32]bool, len(learned))
		for _, id := range learned {
			have[id] = true
		}
		missing := make([]uint32, 0, len(rows))
		for _, row := range rows {
			if !have[row.ID] {
				missing = append(missing, row.ID)
			}
		}
		return missing, nil
	}
}
