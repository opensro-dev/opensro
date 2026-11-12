/*
===========================================================================

pharaohtemple.go - the Pharaoh Temple (the job temple), port-only identity table.

Port-only, not native; the temple exists only while SRO_PHARAOH_TEMPLE is on.
v1.150 has no temple. The VSRO client has one in the Storm and Cloud Desert:
the main Temple and six sanctums, seven dungeons in all, behind two desert
gates (one for the merchant and hunter jobs, one for the thief job) and two
walk-in gates inside the Temple that lead back out. It is not the temple of
the band worlds (SRO_CAP110_TEMPLE, package temple): that is a different
place with instanced worlds of its own.

The seven dungeons are dungeoninfo ids 10-16, regions 0x800A-0x8010 (the low
fifteen bits are the dungeoninfo id, the high bit marks a dungeon region).
v1.150 uses ids 1 and 9, the Qin-Shi floors 2-7, and the flag-war arena is
0x8009; nothing collides. The Temple itself is id 16 (region 0x8010), the
sanctums 10-15.

The source database groups the Temple's monsters under worlds 60 (Temple)
and 51-56 (sanctums). The port has no such worlds: the Temple is a dungeon
region of the persistent world, as the Donwhang cave is, and the variant
build flattens the gates' world column to 1. SourceWorld keeps the source
ids for the population slice, which maps nests to regions.

This leaf owns the identity table and the switch, and answers one question
for the portal catalogue and the movement authority: is this gate, or this
region, the temple's. It imports nothing from the module.

===========================================================================
*/
package pharaohtemple

import (
	"fmt"
	"os"
	"strings"
)

// EnvPharaohTemple is the shard switch (shardsettings, nomad var
// pharaoh_temple). Off is the native v1.150 shard.
const EnvPharaohTemple = "SRO_PHARAOH_TEMPLE"

// DungeonRegionBit marks a dungeon region; a dungeoninfo id is the rest.
const DungeonRegionBit uint16 = 0x8000

// The dungeons' dungeoninfo ids (10-16) and the Temple's region.
const (
	FirstDungeonID = 10
	LastDungeonID  = 16
	// TempleDungeonID is the main Temple; the other six are sanctums.
	TempleDungeonID = 16
	// TempleRegionID is the Temple's region, 0x8010, the one both
	// walk-in gates stand in.
	TempleRegionID = DungeonRegionBit | TempleDungeonID
)

// The gates' codenames, as teleportdata.txt spells them. The two desert
// gates are objects a player clicks (teleportbuilding); the two temple
// gates are walk-in event zones of the Temple's own objects.
const (
	DesertMerchantHunterGate = "GATE_DESERT_MERCHANT_HUNTER_GATE"
	DesertThiefGate          = "GATE_DESERT_THIEF_GATE"
	TempleMerchantHunterGate = "GATE_TEMPLE_MERCHANT_HUNTER_GATE"
	TempleThiefGate          = "GATE_TEMPLE_THIEF_GATE"
)

// The two desert gates' teleportbuilding codenames: the objects a player
// clicks. The temple gates have no object.
const (
	DesertMerchantHunterBuilding = "STORE_DESERT_MERCHANT_HUNTER_GATE"
	DesertThiefBuilding          = "STORE_DESERT_THIEF_GATE"
)

/*
================
Dungeon

One dungeon of the temple: its dungeoninfo id, its region, its name and
the source database world its monsters are grouped under.
================
*/
type Dungeon struct {
	ID          int
	Region      uint16
	Name        string
	SourceWorld int
}

var dungeons = []Dungeon{
	{10, DungeonRegionBit | 10, "Sanctum of Restriction", 51},
	{11, DungeonRegionBit | 11, "Sanctum of the Blue Eye", 52},
	{12, DungeonRegionBit | 12, "Sanctum of Punishment", 53},
	{13, DungeonRegionBit | 13, "Sanctum of Atonement", 54},
	{14, DungeonRegionBit | 14, "Sanctum of Immortality", 55},
	{15, DungeonRegionBit | 15, "Sanctum of Dark", 56},
	{TempleDungeonID, TempleRegionID, "Pharaoh Temple", 60},
}

var gateCodes = []string{
	DesertMerchantHunterGate, DesertThiefGate, TempleMerchantHunterGate, TempleThiefGate,
}

/*
================
Dungeons

Every dungeon of the temple, in dungeoninfo id order.
================
*/
func Dungeons() []Dungeon {
	out := make([]Dungeon, len(dungeons))
	copy(out, dungeons)
	return out
}

/*
================
GateCodes

The four gates' codenames: the two desert gates, then the two temple gates.
================
*/
func GateCodes() []string {
	out := make([]string, len(gateCodes))
	copy(out, gateCodes)
	return out
}

/*
================
IsRegion

Whether a region is one of the temple's seven dungeons.
================
*/
func IsRegion(region uint16) bool {
	for _, dungeon := range dungeons {
		if dungeon.Region == region {
			return true
		}
	}
	return false
}

/*
================
IsGate

Whether a teleportdata or event zone codename is one of the four gates.
Zone names compare in upper case, the way the navigation meshes spell them.
================
*/
func IsGate(code string) bool {
	for _, gate := range gateCodes {
		if strings.EqualFold(gate, code) {
			return true
		}
	}
	return false
}

/*
================
FromEnv

The switch. The on values are the importer's ("1", "on", "true"); unset,
"0", "off" and "false" are off. Any other non-empty value is refused, as
alexandria.EnabledFromEnv refuses one, so a typo cannot silently run the
native shard.
================
*/
func FromEnv() (bool, error) {
	switch value := strings.ToLower(strings.TrimSpace(os.Getenv(EnvPharaohTemple))); value {
	case "1", "on", "true":
		return true, nil
	case "", "0", "off", "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s=%q: want on or off", EnvPharaohTemple, value)
	}
}

/*
================
Enabled

Whether the temple is on. A value FromEnv refuses reads as off, so a loader
that runs before the boot check shows only native content.
================
*/
func Enabled() bool {
	on, err := FromEnv()
	return err == nil && on
}

/*
================
Hides

Whether the shard hides a teleport while the switch is off: a gate by
codename, or any teleport that lands in a temple region. A teleport the
temple does not own is never hidden here.
================
*/
func Hides(code string, region uint16) bool {
	return !Enabled() && (IsGate(code) || IsRegion(region))
}

/*
================
HidesBuilding

Whether the shard hides a teleportbuilding row (by codename) while the
switch is off: the two desert gates' objects.
================
*/
func HidesBuilding(code string) bool {
	return !Enabled() && (code == DesertMerchantHunterBuilding || code == DesertThiefBuilding)
}
