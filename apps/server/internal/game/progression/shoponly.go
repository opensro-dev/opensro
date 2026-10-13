/*
===========================================================================

shoponly.go - degree-9 alchemy goods only from the shop (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-13). With
SRO_BETA_SHOP_ONLY_DEGREE9 on, degree-9 alchemy goods no longer drop from
monsters; players buy them from the Jangan shops. Degrees 1-8 and every
other drop are unchanged. Off, every drop rolls exactly as before.

"Degree 9" is the item's own itemdata degree, the ItemClass column (61)
alchemy goods carry, and the families are told apart by their type IDs
(TID 3/3/10/2 Lucky Powder, 3/3/11/1 stat magic stones, 3/3/11/2
attribute stones, 3/3/11/7 the smith's Luck, Steady, Astral, Athanasia
and Repair stones). v1.150's drop catalogue carries Lucky Powder at no
degree and Astral, Athanasia and Repair at none either, so for them the
rule only holds the line should a drop table list them. v1.150's elixirs (3/3/10/1) have no degree, only A/B grades, so
none is degree 9.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvBetaShopOnlyDegree9 turns the rule on ("on", "1", "true").
const EnvBetaShopOnlyDegree9 = "SRO_BETA_SHOP_ONLY_DEGREE9"

// shopOnlyDegree is the alchemy degree that only the shop sells.
const shopOnlyDegree = 9

// shopOnlyFamilies are the alchemy families' last three type IDs (the
// first is 3, an etc item): Lucky Powder, stat magic stones, attribute
// stones, and the Luck, Steady, Astral, Athanasia and Repair stones.
var shopOnlyFamilies = [][3]int64{{3, 10, 2}, {3, 11, 1}, {3, 11, 2}, {3, 11, 7}}

/*
================
ShopOnlyDegree9

The zero value is off.
================
*/
type ShopOnlyDegree9 struct {
	enabled bool
}

/*
================
BetaShopOnlyDegree9FromEnv
================
*/
func BetaShopOnlyDegree9FromEnv() ShopOnlyDegree9 {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaShopOnlyDegree9))) {
	case "on", "1", "true":
		return ShopOnlyDegree9{enabled: true}
	}
	return ShopOnlyDegree9{}
}

/*
================
ShopOnlyDegree9.Enabled
================
*/
func (s ShopOnlyDegree9) Enabled() bool { return s.enabled }

/*
================
ShopOnlyDegree9.Excludes

Whether an item of these type IDs and ItemClass may no longer drop.
================
*/
func (s ShopOnlyDegree9) Excludes(typeIDs [4]int64, itemClass int) bool {
	if !s.enabled || typeIDs[0] != 3 || itemClass != shopOnlyDegree {
		return false
	}
	for _, family := range shopOnlyFamilies {
		if typeIDs[1] == family[0] && typeIDs[2] == family[1] && typeIDs[3] == family[2] {
			return true
		}
	}
	return false
}
