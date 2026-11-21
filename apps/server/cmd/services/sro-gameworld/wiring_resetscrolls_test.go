/*
===========================================================================

wiring_resetscrolls_test.go - a reset scroll lists only with its item

Port-only, not native. A switch on whose item the build did not stage
lists nothing and turns its use off; the boot goes on.

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
resetScrollRefs

An item source holding only the codenames it is given.
================
*/
type resetScrollRefs map[string]*enterworld.ItemRef

func (refs resetScrollRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := refs[codename]
	return ref, ok
}

func (refs resetScrollRefs) ItemRefByID(uint32) (*enterworld.ItemRef, bool) { return nil, false }

/*
================
TestApplyResetScrollsSkipsMissingItems
================
*/
func TestApplyResetScrollsSkipsMissingItems(t *testing.T) {
	t.Setenv(commerce.EnvStatResetScroll, "on")
	t.Setenv(commerce.EnvSkillResetScroll, "on")
	items := &action.Runtime{}
	refs := resetScrollRefs{"ITEM_MALL_SKILL_POINT_RECALL_A": {Codename: "ITEM_MALL_SKILL_POINT_RECALL_A"}}
	goods := applyResetScrolls(items, refs)
	if len(goods) != 1 || goods[0].Item != "ITEM_MALL_SKILL_POINT_RECALL_A" {
		t.Fatalf("goods = %+v, want the skill scroll alone", goods)
	}
	if items.ResetScrolls[commerce.StatReset] || !items.ResetScrolls[commerce.SkillReset] {
		t.Fatalf("enabled = %+v, want the skill scroll alone", items.ResetScrolls)
	}
	t.Setenv(commerce.EnvSkillResetScroll, "")
	items = &action.Runtime{}
	if goods := applyResetScrolls(items, refs); len(goods) != 0 || items.ResetScrolls[commerce.SkillReset] {
		t.Fatalf("switch off lists %+v, enabled %+v; want nothing", goods, items.ResetScrolls)
	}
}
