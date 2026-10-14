/*
===========================================================================

sunevent.go - degree-9 Seal of Sun gear only from events (port-only, not native)

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_SUN_EVENT_ONLY on, no monster drops degree-9 Seal of Sun gear and
no NPC sells it; it returns only as an event reward (granted by staff).
A Sun pick drops nothing rather than being re-rolled into another item, so
every other item keeps its native chance. Sun gear players already own is
kept as it is. Off, every drop and every shop is native.

A seal's tier and degree are the item's ItemClass (itemdata column 61), as
the client reads them (progression/sealrates.go): degree 9 is classes
25-27 and Sun the third, class 27. The ordinary degree-9 item shares class
27, so only a sealed item (rarity, column 15, 2) is Sun. v1.150 data: 36
such rows, the honor shop's *_09_C_RARE_HONOR packages among them.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvSunEventOnly turns the rule on ("on", "1", "true").
const EnvSunEventOnly = "SRO_SUN_EVENT_ONLY"

// sunEventOnlyDegree is the one seal degree the rule covers (the balance
// advisor, 2026-10-14: degree 9 only).
const sunEventOnlyDegree = 9

// itemRaritySealed is RefObjCommon rarity 2, a sealed (SoX) item.
const itemRaritySealed = 2

// itemTypeEquipment is TypeID2 of every piece of equipment.
const itemTypeEquipment = 1

/*
================
SunEventOnly

The zero value is off.
================
*/
type SunEventOnly struct {
	enabled bool
}

/*
================
SunEventOnlyFromEnv
================
*/
func SunEventOnlyFromEnv() SunEventOnly {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSunEventOnly))) {
	case "on", "1", "true":
		return SunEventOnly{enabled: true}
	}
	return SunEventOnly{}
}

/*
================
SunEventOnly.Enabled
================
*/
func (s SunEventOnly) Enabled() bool { return s.enabled }

/*
================
SunEventOnly.Excludes

Whether an item of these type IDs, rarity and ItemClass may no longer drop
or be sold.
================
*/
func (s SunEventOnly) Excludes(typeIDs [4]int64, rarity, itemClass int) bool {
	return s.enabled && typeIDs[0] == 3 && typeIDs[1] == itemTypeEquipment && rarity == itemRaritySealed &&
		itemClass == sealItemClass(sunEventOnlyDegree, SealSun)
}

/*
================
sealItemClass

The ItemClass of a degree's seal: (degree - 1) * 3 + seal + 1, the inverse
of the client's (ItemClass - 1) % 3 and (ItemClass + 2) / 3.
================
*/
func sealItemClass(degree, seal int) int {
	return (degree-1)*sealClassesPerDegree + seal + 1
}
