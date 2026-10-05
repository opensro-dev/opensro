/*
===========================================================================

caravan.go - the bandits that ambush a trade caravan

Caravan_SpawnBandits (60BF30) chooses each bandit from one of two reference
tables and spawns it with a numbered tactics row. Both tables are built from
the monster references the way the native refdata does it
(CRefData_BuildThiefBanditTable 6BB8A0, CRefData_BuildHunterBanditTable
6BC5A0); the eight tactics rows are the vSRO backup's Tab_RefTactics
2001..2004 and 2011..2014 (scripts/build/import_caravan_evidence.py).

===========================================================================
*/

package monster

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	// BanditZones is the zone count the native lookups assert (zone < 3).
	BanditZones = 3
	// banditLevels is the bucket count: levels 1..140 (index 0..0x8B).
	banditLevels = 0x8c
	// caravanTacticsSource pins the shard backup the rows come from.
	caravanTacticsSource = "9b9179e598f303f3293df1771ed7a9339a5e9c9de0dd2401884db2817d5dd9a4"
)

//go:embed data/caravan_tactics.json
var caravanTacticsJSON []byte

var caravanTactics = loadCaravanTactics(caravanTacticsJSON)

/*
================
loadCaravanTactics

The embedded rows are a build input: a missing or malformed row is a
build defect, not a runtime error.
================
*/
func loadCaravanTactics(data []byte) map[uint32]SummonTactics {
	var doc struct {
		Source  string
		Tactics map[string]TacticsControls
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		panic(fmt.Errorf("caravan tactics: %w", err))
	}
	if doc.Source != caravanTacticsSource || len(doc.Tactics) != 8 {
		panic("caravan tactics provenance/closure mismatch")
	}
	out := make(map[uint32]SummonTactics, len(doc.Tactics))
	for key, c := range doc.Tactics {
		thief := c.ID >= 2001 && c.ID <= 2004
		hunter := c.ID >= 2011 && c.ID <= 2014
		if fmt.Sprint(c.ID) != key || (!thief && !hunter) || c.SightRange < 0 || c.ChampionID != 0 {
			panic("invalid caravan tactics row " + key)
		}
		// The same projection the summon factory uses: ChangeTarget is the
		// target policy byte (+0x20), AdditionOptionFlag the native flags.
		out[c.ID] = SummonTactics{
			TargetPolicy: c.ChangeTarget, SightRange: float64(c.SightRange),
			NativeFlags: c.Flags, Controls: c, HasControls: true,
		}
	}
	return out
}

/*
================
CaravanTactics

One of the eight bandit tactics rows by its native number.
================
*/
func CaravanTactics(id uint32) (SummonTactics, bool) {
	tactics, ok := caravanTactics[id]
	return tactics, ok
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

6BB8A0 / 6BC5A0: a thief (word 0x10C6) or hunter (0x18C6) reference is
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

6BC4B0 / 6BC760: thieves serve a trader, hunters everyone else. Start at
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
