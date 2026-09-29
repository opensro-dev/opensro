/*
===========================================================================

lootreferences.go - fail startup when a compiled reward cannot be published

The composition root checks this once after loading the v1.150 reference plane.
Partial references must never silently reduce live monster loot.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/game/item/loot"
)

/*
================
ValidateLootReferences
================
*/
func (rt *Runtime) ValidateLootReferences() error {
	items := rt.deps.ItemReferences()
	if items == nil {
		return fmt.Errorf("loot requires the client item reference catalog")
	}
	for _, code := range loot.CatalogItemCodenames() {
		ref, ok := items.ItemRefByCodename(code)
		if !ok || ref == nil || ref.RefObjID == 0 || ref.Codename != code {
			return fmt.Errorf("loot item reference missing: %s", code)
		}
	}
	magic := rt.deps.MagicOptionDefinitions()
	if magic == nil {
		return fmt.Errorf("loot requires the client magic-option reference catalog")
	}
	return loot.ValidateMagicReferences(func(id uint32, name string, degree int) bool {
		ref, ok := magic.MagicOptionByParamID(id)
		return ok && ref != nil && ref.OptionName == name && ref.Degree == int64(degree)
	})
}
