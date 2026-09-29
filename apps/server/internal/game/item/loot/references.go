/*
===========================================================================

references.go - complete server-side loot reference inventory

Startup validates the entire catalog. Client reference publication remains
incremental at ground spawn; this list does not enlarge login packets.

===========================================================================
*/
package loot

import "sort"

/*
================
CatalogItemCodenames
================
*/
func CatalogItemCodenames() []string {
	seen := map[string]bool{"ITEM_ETC_SCROLL_RETURN_02": true}
	for _, kind := range [...]string{"WEAPON", "SHIELD", "ARMOR", "ACCESSARY"} {
		seen["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_"+kind+"_B"] = true
	}
	for _, bucket := range equipment.buckets {
		for _, ref := range bucket.refs {
			seen[ref.Codename] = true
		}
	}
	for _, family := range consumables.families {
		for _, bucket := range family.buckets {
			for _, ref := range bucket.refs {
				seen[ref.Codename] = true
			}
		}
	}
	for _, rows := range consumables.fixed {
		for _, row := range rows {
			seen[row.Item] = true
		}
	}
	for _, rows := range consumables.groups {
		for _, row := range rows {
			seen[row.Codename] = true
		}
	}
	result := make([]string, 0, len(seen))
	for code := range seen {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}
