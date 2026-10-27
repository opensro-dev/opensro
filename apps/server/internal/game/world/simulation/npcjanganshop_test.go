/*
===========================================================================

npcjanganshop_test.go - Jangan's alchemy shop roster (port-only, not native)

The regrouped grocer matches the tabs commerce stocks; every other
switch combination, and every other NPC, keeps the authored roster.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
janganRoster

The shipped roster under this test's environment.
================
*/
func janganRoster(t *testing.T) map[string]NpcDef {
	t.Helper()
	roster := LoadNpcWorldRoster(licensed.RetailTextdataDir(t))
	if len(roster) < 150 {
		t.Skipf("shipped NPC world data unavailable (loaded %d rows)", len(roster))
	}
	byCode := make(map[string]NpcDef, len(roster))
	for _, npc := range roster {
		byCode[npc.Codename] = npc
	}
	return byCode
}

/*
================
grocerTabs
================
*/
func grocerTabs(npc NpcDef) []NpcTalkStoreTab {
	var tabs []NpcTalkStoreTab
	for _, group := range npc.NpcTalkStoreGroups {
		tabs = append(tabs, group.Tabs...)
	}
	return tabs
}

/*
================
TestJanganAlchemyShopRenamesAndRegroupsTheGrocer
================
*/
func TestJanganAlchemyShopRenamesAndRegroupsTheGrocer(t *testing.T) {
	t.Setenv(envJanganAlchemyShop, "on")
	t.Setenv(envShopExtraGoods, "on")
	byCode := janganRoster(t)
	grocer := byCode[janganGrocerCodename]
	if grocer.Name != janganAlchemyName {
		t.Fatalf("grocer name = %q", grocer.Name)
	}
	tabs := grocerTabs(grocer)
	if len(tabs) != 5 {
		t.Fatalf("grocer has %d tabs", len(tabs))
	}
	wantLabels := []string{"SN_TAB_ACCESSORY", "SN_TAB_ETC",
		janganElixirsLabel, janganAttrStonesLabel, janganMagicStonesLabel}
	for i, want := range wantLabels {
		if tabs[i].LabelSymbol != want {
			t.Fatalf("tab %d label = %q, want %q", i, tabs[i].LabelSymbol, want)
		}
	}
	// The synthesized ids sit above every authored id: the shipped
	// tables' largest tab id is 2378, so the tabs are 2379-2381 (the
	// shop catalogue derives the same ids; commerce pins them too),
	// consecutive in tab order under one new group above the authored
	// group maximum.
	var maxGroup int32
	for code, npc := range byCode {
		for i, tab := range grocerTabs(npc) {
			if code == janganGrocerCodename && i >= 2 {
				continue
			}
			if tab.GroupID > maxGroup {
				maxGroup = tab.GroupID
			}
		}
	}
	for i := 0; i < 3; i++ {
		tab := tabs[2+i]
		if tab.TabID != int32(2379+i) || tab.GroupID != maxGroup+1 || tab.GroupLabelSymbol != janganAlchemyGroupLabel {
			t.Fatalf("synthesized tab %d = %+v (authored group maximum %d)", i, tab, maxGroup)
		}
	}
	// The native tabs keep their authored places and group.
	if tabs[0].GroupID != tabs[1].GroupID || tabs[1].GroupID == tabs[2].GroupID {
		t.Fatalf("tab groups = %+v", tabs)
	}
	// Nobody else changed: the smith keeps his tabs, another town's grocer
	// keeps her name.
	if tabs := grocerTabs(byCode["NPC_CH_SMITH"]); len(tabs) != 3 {
		t.Fatalf("smith has %d tabs", len(tabs))
	}
	if other := byCode["NPC_WC_ACCESSORY"]; other.Name == janganAlchemyName || len(grocerTabs(other)) != 3 {
		t.Fatalf("Donwhang grocer = %+v", other)
	}
}

/*
================
TestJanganAlchemyShopOffKeepsTheAuthoredGrocer
================
*/
func TestJanganAlchemyShopOffKeepsTheAuthoredGrocer(t *testing.T) {
	for _, tc := range []struct{ feature, extra string }{
		{"off", "on"},
		{"on", "off"},
		{"on", ""},
		{"banana", "on"},
		{"on", "STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100"},
	} {
		t.Setenv(envJanganAlchemyShop, tc.feature)
		t.Setenv(envShopExtraGoods, tc.extra)
		grocer := janganRoster(t)[janganGrocerCodename]
		if grocer.Name == janganAlchemyName {
			t.Fatalf("feature %q, extra %q renamed the grocer", tc.feature, tc.extra)
		}
		tabs := grocerTabs(grocer)
		if len(tabs) != 3 || tabs[2].LabelSymbol != janganNativeAlchemyLabel {
			t.Fatalf("feature %q, extra %q tabs = %+v", tc.feature, tc.extra, tabs)
		}
	}
}
