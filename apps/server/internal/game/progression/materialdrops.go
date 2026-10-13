/*
===========================================================================

materialdrops.go - no alchemy material drops (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-14, #bug-reports
"garbage is piling up ... remove this alchemy drops"). With
SRO_ALCHEMY_MATERIAL_DROPS_OFF on, no monster drops an alchemy material:
the grey bags of monster parts (claws, hides) that the beta's raised drop
rolls spread over every hunting ground. Their native use is the old stone
chain (break them with a rondo into elements, apply elements to tablets);
on the beta the stones are bought (SRO_SHOP_EXTRA_GOODS), so a material
is only shop junk. Quest rewards are not drops and are unchanged. Off,
every drop rolls exactly as before.

An alchemy material is the alchemy family of type IDs 3/3/11/4. v1.150's
itemdata holds 651 items of it (ITEM_ETC_ARCHEMY_MATERIAL_*). Tablets
(3/3/11/3), elements (3/3/11/5) and rondos (3/3/11/6) are other families
and keep their drops.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvAlchemyMaterialDropsOff turns the rule on ("on", "1", "true").
const EnvAlchemyMaterialDropsOff = "SRO_ALCHEMY_MATERIAL_DROPS_OFF"

// materialTypeIDs are an alchemy material's last three type IDs (the first
// is 3, an etc item).
var materialTypeIDs = [3]int64{3, 11, 4}

/*
================
MaterialDrops

The zero value is off: alchemy materials drop as native.
================
*/
type MaterialDrops struct {
	off bool
}

/*
================
MaterialDropsFromEnv
================
*/
func MaterialDropsFromEnv() MaterialDrops {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAlchemyMaterialDropsOff))) {
	case "on", "1", "true":
		return MaterialDrops{off: true}
	}
	return MaterialDrops{}
}

/*
================
MaterialDrops.Off
================
*/
func (m MaterialDrops) Off() bool { return m.off }

/*
================
MaterialDrops.Excludes

Whether an item of these type IDs may no longer drop.
================
*/
func (m MaterialDrops) Excludes(typeIDs [4]int64) bool {
	return m.off && typeIDs[0] == 3 && typeIDs[1] == materialTypeIDs[0] &&
		typeIDs[2] == materialTypeIDs[1] && typeIDs[3] == materialTypeIDs[2]
}
