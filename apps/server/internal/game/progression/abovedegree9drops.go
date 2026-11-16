/*
===========================================================================

abovedegree9drops.go - no monster drops above degree 9 (port-only, not native)

Port-only, not native (owner 2026-11-16, #general). The areas above
degree 9 (Alexandria, Baghdad, the Jangan cave and the cap-110 monster
families) stay open as a preview while the level cap stays 90, and three
community polls decide what they should drop. Until they close no new
degree-10 or degree-11 item may enter the economy: at Lv90 the first
degree-10 step of weapons, shields, rings, gloves and shoulders can be
worn and outclasses degree-9 gear. With SRO_ABOVE_DEGREE9_DROPS_OFF on,
a monster's pick of such an item drops nothing and is never re-rolled,
so every other chance, degree 9 and below, is unchanged. Quest rewards
are not drops and are unchanged. Off, every drop rolls exactly as before.

What carries a degree (enterworld.ItemDegree): equipment, by its
ItemClass, and the degree-banded alchemy goods of families 3/3/10
(elixirs, Lucky Powder) and 3/3/11 (magic, attribute and special stones,
tablets, elements, materials), whose ItemClass is the degree. Other etc
items reuse ItemClass for something else (pet collars 3/3/9 run up to 17,
a speed potion 3/3/13 is 11), so they never count.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvAboveDegree9DropsOff turns the rule on ("on", "1", "true").
const EnvAboveDegree9DropsOff = "SRO_ABOVE_DEGREE9_DROPS_OFF"

// MaxDropDegree is the highest degree a monster may drop while the rule is
// on: the top degree of the level-90 cap.
const MaxDropDegree = 9

// The type IDs that carry a degree: equipment (1/x/x under the item type
// 3), and the etc item families 3/3/10 and 3/3/11 (alchemy goods).
const (
	itemTypeID         = 3
	equipmentTypeID    = 1
	etcTypeID          = 3
	alchemyGoodsTypeID = 10
	alchemyStoneTypeID = 11
)

/*
================
AboveDegree9Drops

The zero value is off: every degree drops as before.
================
*/
type AboveDegree9Drops struct {
	off bool
}

/*
================
AboveDegree9DropsFromEnv
================
*/
func AboveDegree9DropsFromEnv() AboveDegree9Drops {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAboveDegree9DropsOff))) {
	case "on", "1", "true":
		return AboveDegree9Drops{off: true}
	}
	return AboveDegree9Drops{}
}

/*
================
AboveDegree9Drops.Off
================
*/
func (d AboveDegree9Drops) Off() bool { return d.off }

/*
================
CarriesDegree

Whether an item of these type IDs has a degree that its ItemClass names
(equipment, or the alchemy goods families).
================
*/
func CarriesDegree(typeIDs [4]int64) bool {
	if typeIDs[0] != itemTypeID {
		return false
	}
	if typeIDs[1] == equipmentTypeID {
		return true
	}
	return typeIDs[1] == etcTypeID && (typeIDs[2] == alchemyGoodsTypeID || typeIDs[2] == alchemyStoneTypeID)
}

/*
================
AboveDegree9Drops.Excludes

Whether an item of these type IDs and this degree (enterworld.ItemDegree)
may no longer drop.
================
*/
func (d AboveDegree9Drops) Excludes(typeIDs [4]int64, degree int) bool {
	return d.off && degree > MaxDropDegree && CarriesDegree(typeIDs)
}
