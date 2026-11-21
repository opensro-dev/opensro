/*
===========================================================================

wiring_itemmall.go - the Item Mall's catalogue and its port-only goods

The mall lists the client's own packages (ConfigureMall reads them from
textdata) plus the port-only goods each switch adds: the authored extra
packages, the name change scroll, the equipment switch scrolls. This file
gathers those goods, picks the category layout and configures the mall
once at boot; newGameplayPlane calls it after the commerce catalogue.

===========================================================================
*/
package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
configureItemMall

The mall's goods and layout from the switches, then the catalogue. The
stat recall windows follow from what the mall lists.
================
*/
func configureItemMall(items *action.Runtime, textdataDir string, mallAuthority domain.MallAuthority, refs enterworld.ItemRefSource) error {
	// SRO_MALL_EXTRA_GOODS (port-only, not native): authored packages the
	// v1.150 mall does not list; "off" is the shipped mall (commerce/mallextra.go).
	mallExtra, err := commerce.MallExtraGoodsFromEnv()
	if err != nil {
		return err
	}
	if len(mallExtra) > 0 {
		log.Infof("mall: %d extra Item Mall goods from %s", len(mallExtra), commerce.EnvMallExtraGoods)
	}
	// SRO_NAME_CHANGE_SCROLL (port-only, not native, #1689): the mall
	// sells the name-change scroll and the action runtime serves its use.
	items.NameChangeScroll = action.NameChangeScrollFromEnv()
	if items.NameChangeScroll {
		mallExtra = append(mallExtra, commerce.MallExtraGood{
			Tab: action.NameChangeMallTab, Package: action.NameChangeMallPackage,
			Item: action.NameChangeScrollItem, Silk: action.NameChangePriceSilk,
		})
		log.Infof("mall: name change scroll listed at %d silk (%s, port-only, not native)", action.NameChangePriceSilk, action.EnvNameChangeScroll)
	}
	// The equipment switch scrolls (port-only, wiring_equipswitch.go).
	mallExtra = append(mallExtra, applyEquipSwitchScrolls(items, refs)...)
	// SRO_MALL_CATEGORIES (port-only, not native, #3257): the later
	// mall's categories, HOT shop and goods badges; "off" (the
	// default) is the shipped mall (commerce/mallcategories.go).
	mallCategories, err := commerce.MallCategoriesFromEnv()
	if err != nil {
		return err
	}
	if mallCategories {
		log.Infof("mall: later category layout from %s (port-only, not native)", commerce.EnvMallCategories)
	}
	if err := items.ConfigureMall(textdataDir, mallAuthority, mallExtra, mallCategories); err != nil {
		return fmt.Errorf("mall catalogue: %w", err)
	}
	items.StatRecallWindows = commerce.StatRecallListed(mallExtra)
	return nil
}
