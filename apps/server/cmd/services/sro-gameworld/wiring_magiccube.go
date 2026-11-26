/*
===========================================================================

wiring_magiccube.go - Magic Cube switch listing on the Item Mall

Port-only, not native (#3906). SRO_MAGIC_CUBE (game/magiccube) already
admits deposit/withdraw on the action runtime (wiring_gameplay.go). This
file lists the sixteen cubes on the normal Silk Mall only while the
switch is on and the build staged their rows (scripts/magic_cube_import.py);
unset keeps the native mall.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/magiccube"
)

/*
================
applyMagicCubeMall

Returns the sixteen cube mall listings while the switch is on and every
cube stands in the data. A switch on without the items logs one warning
and lists nothing; the boot goes on.
================
*/
func applyMagicCubeMall(items *action.Runtime, refs enterworld.ItemRefSource) []commerce.MallExtraGood {
	if !items.MagicCube {
		return nil
	}
	if refs == nil {
		log.Warnf("mall: %s is on but no item data is loaded; Magic Cubes are not listed", magiccube.Env)
		return nil
	}
	for _, cube := range commerce.MagicCubeListings {
		if _, found := refs.ItemRefByCodename(cube.Codename); !found {
			log.Warnf("mall: %s is on but the build staged no %s (scripts/magic_cube_import.py); not listed",
				magiccube.Env, cube.Codename)
			return nil
		}
	}
	log.Infof("mall: %d Magic Cubes listed on %s (%s, port-only, not native)",
		len(commerce.MagicCubeListings), "MALL_CONSUME_ETC", magiccube.Env)
	return commerce.MagicCubeMallGoods(true)
}
