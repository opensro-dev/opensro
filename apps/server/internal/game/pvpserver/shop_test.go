/*
===========================================================================

shop_test.go - Samarkand's PVP shop layout

===========================================================================
*/

package pvpserver

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func weapon(code string) ShopItem {
	return ShopItem{Codename: code, TypeID1: 3, TypeID2: 1, TypeID3: 6}
}
func armor(code string, kind int) ShopItem {
	return ShopItem{Codename: code, TypeID1: 3, TypeID2: 1, TypeID3: kind}
}
func stone(code string) ShopItem {
	return ShopItem{Codename: code, TypeID1: 3, TypeID2: 3, TypeID3: 11}
}

func TestLayoutSplitsByNpcAndTab(t *testing.T) {
	var items []ShopItem
	for i := 0; i < 50; i++ {
		items = append(items, weapon(fmt.Sprintf("ITEM_EU_SWORD%02d_11_A_RARE", i)))
	}
	items = append(items,
		weapon("ITEM_EU_AXE_11_A_RARE_ACTIVE"),
		weapon("ITEM_EU_AXE_11_A_RARE_HONOR"),
		weapon("ITEM_EU_AXE_10_A_RARE"),
		weapon("ITEM_EU_AXE_11_A"),
		ShopItem{Codename: "ITEM_EU_SHIELD_11_SET_A_RARE", TypeID1: 3, TypeID2: 1, TypeID3: 4},
		armor("ITEM_EU_M_HEAVY_11_SA_SET_B_RARE", 11),
		armor("ITEM_CH_M_CLOTHES_11_SA_A_RARE", 1),
		ShopItem{Codename: "ITEM_EU_RING_11_A_RARE", TypeID1: 3, TypeID2: 1, TypeID3: 12},
		stone("ITEM_ETC_ARCHEMY_ATTRSTONE_PA_11"),
		stone("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_11"),
		stone("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09"),
	)

	tabs := Layout(items, 100, 50)

	var smith []ShopTab
	byLabel := map[string]ShopTab{}
	for _, tab := range tabs {
		if tab.Npc == "NPC_CA_SMITH" && tab.Label == "SN_TAB_WEAPON" {
			smith = append(smith, tab)
		}
		byLabel[tab.Npc+"/"+tab.Label] = tab
	}
	if len(smith) != 2 || len(smith[0].Codenames) != TabCapacity || len(smith[1].Codenames) != 5 {
		t.Fatalf("weapon tabs = %+v", smith)
	}
	for _, tab := range tabs {
		for _, code := range tab.Codenames {
			if strings.Contains(code, "_ACTIVE") || strings.Contains(code, "_HONOR") ||
				strings.Contains(code, "_10_") || code == "ITEM_EU_AXE_11_A" || strings.HasSuffix(code, "_09") {
				t.Fatalf("tab %s sells %s", tab.Label, code)
			}
		}
	}
	if smith[0].Plus != ShopPlus || byLabel["NPC_CA_ACCESSORY/UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE"].Plus != 0 {
		t.Fatal("gear must be +12 and stones +0")
	}
	for _, key := range []string{
		"NPC_CA_SMITH/SN_TAB_SHIELD",
		"NPC_CA_ARMOR/SN_TAB_HEAVYARMOR_EU",
		"NPC_CA_ARMOR/SN_TAB_CLOTHES_EU",
		"NPC_CA_ACCESSORY/SN_TAB_ACCESSORY",
		"NPC_CA_ACCESSORY/UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE",
		"NPC_CA_ACCESSORY/UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE",
	} {
		if len(byLabel[key].Codenames) != 1 {
			t.Fatalf("%s = %+v", key, byLabel[key])
		}
	}
	seen := map[int32]bool{}
	for _, tab := range tabs {
		if tab.TabID <= 100+janganReservedTabs || seen[tab.TabID] {
			t.Fatalf("tab id %d collides", tab.TabID)
		}
		seen[tab.TabID] = true
		if tab.GroupID <= 50+janganReservedGroups {
			t.Fatalf("group id %d collides", tab.GroupID)
		}
	}
}

func writeUTF16(t *testing.T, path string, lines ...string) {
	t.Helper()
	units := utf16.Encode([]rune(strings.Join(lines, "\r\n") + "\r\n"))
	data := []byte{0xff, 0xfe}
	for _, unit := range units {
		data = binary.LittleEndian.AppendUint16(data, unit)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func itemRow(code string, types ...int) string {
	cols := make([]string, 13)
	for i := range cols {
		cols[i] = "0"
	}
	cols[0], cols[2] = "1", code
	cols[9], cols[10], cols[11] = fmt.Sprint(types[0]), fmt.Sprint(types[1]), fmt.Sprint(types[2])
	return strings.Join(cols, "\t")
}

func TestShopLayoutReadsTheTables(t *testing.T) {
	dir := t.TempDir()
	writeUTF16(t, filepath.Join(dir, "itemdata.txt"), "ItemData_5000.txt")
	writeUTF16(t, filepath.Join(dir, "itemdata_5000.txt"),
		itemRow("ITEM_EU_SWORD_11_A_RARE", 3, 1, 6),
		"0\t0\tITEM_EU_SPEAR_11_A_RARE\t0\t0\t0\t0\t0\t0\t3\t1\t6\t0")
	writeUTF16(t, filepath.Join(dir, "refshoptab.txt"), "1\t11\t2037\tA\tB\tC", "1\t11\t6140\tA\tB\tC")
	writeUTF16(t, filepath.Join(dir, "refshoptabgroup.txt"), "1\t11\t2712\tA\tB")

	tabs, err := ShopLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].TabID != 6140+janganReservedTabs+1 || tabs[0].GroupID != 2712+janganReservedGroups+1 {
		t.Fatalf("tabs = %+v", tabs)
	}
	if len(tabs[0].Codenames) != 1 || tabs[0].Codenames[0] != "ITEM_EU_SWORD_11_A_RARE" {
		t.Fatalf("an unserviced row was sold: %v", tabs[0].Codenames)
	}
}
