/*
===========================================================================

itemrefs_icons.go - drawn-icon resolution for item references

The one authored icon reference that ships in no media, and the per-item
substitute art a reference resolves to instead (#3216). The item rows
themselves are built in itemrefs.go; this file owns only what the drawn
icon becomes on the way out.

===========================================================================
*/

package enterworld

import "strings"

// danglingReinforceRecipeIcon is the one AssocFileIcon the four regular
// (A-grade) reinforcement elixirs reference (itemdata_5000.txt:1519-1522).
// The referenced file ships in no media: #2790 verified it missing from the
// v1.150 data, the pinned later client and v1.188/iSRO-R data, so retail
// draws its fallback plate for all four.
const danglingReinforceRecipeIcon = `item\etc\archemy_reinforce_recipe_a.ddj`

// reinforceRecipeIcons maps each regular elixir to the icon its type's
// B-grade row references (itemdata_5000.txt:1523-1526); those four files
// ship. The publisher-side substitute (#2790) cannot split the four: it is
// keyed by the shared referenced path, so it published the generic B-grade
// art under the referenced name and every regular elixir drew it.
var reinforceRecipeIcons = map[string]string{
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A":    `item\etc\archemy_reinforce_recipe_weapon_b.ddj`,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A":    `item\etc\archemy_reinforce_recipe_shield_b.ddj`,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A":     `item\etc\archemy_reinforce_recipe_armor_b.ddj`,
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A": `item\etc\archemy_reinforce_recipe_accessary_b.ddj`,
}

/*
================
reinforceRecipeIcon

The drawn icon for one item reference. Only an item whose authored icon is
the dangling shared reference above is rewritten, each regular elixir to
its own type's shipped art; every other reference is returned untouched.

This refines the #2790 substitute (owner decision 2026-11-08: draw named
substitute art, never the NO IMAGE plate) per item: the publisher could
only swap art for the referenced name as a whole, but the item rows are
built here, where the elixir type is known.
================
*/
func reinforceRecipeIcon(codename string, icon string) string {
	referenced := strings.ReplaceAll(icon, "/", `\`)
	if !strings.EqualFold(referenced, danglingReinforceRecipeIcon) {
		return icon
	}
	if own, ok := reinforceRecipeIcons[codename]; ok {
		return own
	}
	return icon
}
