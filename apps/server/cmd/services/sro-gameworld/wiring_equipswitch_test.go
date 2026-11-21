/*
===========================================================================

wiring_equipswitch_test.go - the switch scrolls' flags meet the item data

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
oneItemRefs

An item table holding exactly one codename.
================
*/
type oneItemRefs struct{ codename string }

/*
================
oneItemRefs.ItemRefByCodename
================
*/
func (r oneItemRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if codename != r.codename {
		return nil, false
	}
	return &enterworld.ItemRef{Codename: codename}, true
}

/*
================
oneItemRefs.ItemRefByID
================
*/
func (r oneItemRefs) ItemRefByID(uint32) (*enterworld.ItemRef, bool) { return nil, false }

/*
================
TestApplyEquipSwitchScrollsSkipsMissingItems

Every flag on, but the build staged only the armor scroll's item: only
the armor scroll is served and listed.
================
*/
func TestApplyEquipSwitchScrollsSkipsMissingItems(t *testing.T) {
	for _, scroll := range commerce.EquipSwitchScrolls {
		t.Setenv(scroll.Env, "on")
	}
	items := &action.Runtime{}
	goods := applyEquipSwitchScrolls(items, oneItemRefs{codename: "ITEM_MALL_ARMOR_SWITCH_SCROLL"})
	if len(goods) != 1 || goods[0].Item != "ITEM_MALL_ARMOR_SWITCH_SCROLL" {
		t.Fatalf("goods %+v, want the armor scroll alone", goods)
	}
	if !items.EquipSwitch[commerce.SwitchArmor] || items.EquipSwitch[commerce.SwitchWeapon] || items.EquipSwitch[commerce.SwitchAccessory] {
		t.Fatalf("enabled kinds %+v, want armor alone", items.EquipSwitch)
	}
}
