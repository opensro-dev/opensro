/*
===========================================================================

caravan.go - the bandits that ambush a trade caravan

The native bandit spawn chooses each bandit from one of two reference
tables and spawns it with a numbered tactics row. Both tables are built from
the monster references the way the native refdata does it; the eight
tactics rows are the later data's tactics 2001..2004 and 2011..2014 of the
operator's server tactics reference,
carried in the projection's derived data (CaravanTacticsFile); without
them no caravan draws bandits.

===========================================================================
*/

package monster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"opensro.online/server/internal/gamedata"
)

const (
	// BanditZones is the zone count the native lookups assert (zone < 3).
	BanditZones = 3
	// banditLevels is the bucket count: levels 1..140 (index 0..0x8B).
	banditLevels = 0x8c
	// caravanTacticsRows is the closed set the native binds by number:
	// 2001..2004 for thieves, 2011..2014 for hunters.
	caravanTacticsRows = 8
)

// CaravanTacticsFile is the bandit tactics table in the derived data.
const CaravanTacticsFile = "monster/caravan-tactics.json"

/*
================
CaravanTacticsTable

The eight bandit tactics rows by native number. A nil table has none.
================
*/
type CaravanTacticsTable map[uint32]SummonTactics

/*
================
LoadCaravanTactics

The table from derivedDir, or nil when there is none.
================
*/
func LoadCaravanTactics(derivedDir string) (CaravanTacticsTable, error) {
	data, err := gamedata.ReadDerived(derivedDir, CaravanTacticsFile)
	if err != nil || data == nil {
		return nil, err
	}
	return parseCaravanTactics(data)
}

/*
================
parseCaravanTactics

Exactly the eight native numbers, each with valid controls: a missing,
foreign or malformed row refuses the whole table.
================
*/
func parseCaravanTactics(data []byte) (CaravanTacticsTable, error) {
	var doc struct {
		Source  string
		Tactics map[string]TacticsControls
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("caravan tactics: %w", err)
	}
	if len(doc.Tactics) != caravanTacticsRows {
		return nil, fmt.Errorf("caravan tactics: %d rows, want %d", len(doc.Tactics), caravanTacticsRows)
	}
	out := make(CaravanTacticsTable, len(doc.Tactics))
	for key, c := range doc.Tactics {
		thief := c.ID >= 2001 && c.ID <= 2004
		hunter := c.ID >= 2011 && c.ID <= 2014
		if fmt.Sprint(c.ID) != key || (!thief && !hunter) || c.SightRange < 0 || c.ChampionID != 0 {
			return nil, fmt.Errorf("caravan tactics: invalid row %s", key)
		}
		// The same projection the summon factory uses: ChangeTarget is the
		// target policy byte, AdditionOptionFlag the native flags.
		out[c.ID] = SummonTactics{
			TargetPolicy: c.ChangeTarget, SightRange: float64(c.SightRange),
			NativeFlags: c.Flags, Controls: c, HasControls: true,
		}
	}
	return out, nil
}

/*
================
BanditTables

The thief and hunter references bucketed by zone and level, each bucket in
reference-ID order (the native refdata walks its ID-ordered object map).
================
*/
type BanditTables struct {
	thief, hunter [BanditZones][banditLevels][]MonsterRef
}

/*
================
NewBanditTables

The native table builders: a thief (word 0x10C6) or hunter (0x18C6) reference is
filed by its codename prefix (strncmp: MOB_THIEF / MOB_HUNTER zone 0,
MOB_EU_THIEF / MOB_EU_HUNTER zone 1, anything else zone 2) and level.
================
*/
func NewBanditTables(refs []MonsterRef) *BanditTables {
	ordered := append([]MonsterRef(nil), refs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].RefObjID < ordered[j].RefObjID })
	tables := &BanditTables{}
	for _, ref := range ordered {
		level := int(ref.Level) - 1
		if level < 0 || level >= banditLevels {
			continue
		}
		switch NativeTypeWord(ref) & typeWordFlagMask {
		case thiefMonsterTypeWord:
			zone := banditZone(ref.Codename, "MOB_THIEF", "MOB_EU_THIEF")
			tables.thief[zone][level] = append(tables.thief[zone][level], ref)
		case hunterMonsterTypeWord:
			zone := banditZone(ref.Codename, "MOB_HUNTER", "MOB_EU_HUNTER")
			tables.hunter[zone][level] = append(tables.hunter[zone][level], ref)
		}
	}
	return tables
}

/*
================
banditZone
================
*/
func banditZone(codename, eastern, western string) int {
	if strings.HasPrefix(codename, eastern) {
		return 0
	}
	if strings.HasPrefix(codename, western) {
		return 1
	}
	return 2
}

/*
================
Pick

The native pick: thieves serve a trader, hunters everyone else. Start at
the bandit's level (capped at 140) and walk down to the nearest level that
has references; like the native loop, the walk stops before level 1. One
CRT draw picks within the bucket.
================
*/
func (t *BanditTables) Pick(thieves bool, zone uint8, level int, draw func() uint32) (MonsterRef, bool) {
	if t == nil || zone >= BanditZones {
		return MonsterRef{}, false
	}
	table := &t.hunter
	if thieves {
		table = &t.thief
	}
	index := level - 1
	if index < 0 {
		return MonsterRef{}, false
	}
	if index >= banditLevels {
		index = banditLevels - 1
	}
	for {
		bucket := table[zone][index]
		if len(bucket) != 0 {
			return bucket[draw()%uint32(len(bucket))], true
		}
		index--
		if index <= 0 {
			return MonsterRef{}, false
		}
	}
}
