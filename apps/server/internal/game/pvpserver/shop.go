/*
===========================================================================

shop.go - Samarkand's PVP shops: which goods, on which tabs

Port-only, not native. On the PVP shard three Samarkand merchants carry
one more store group each: the smith the degree-11 rare weapons and
shields, the armorer the degree-11 rare armour, the accessory merchant
the degree-11 rare accessories and the degree-11 attribute and magic
stones. Gear is sold +12; stones as they are. The potion merchant adds
two: the top grade of every recovery potion and cure pill, and the
timed item buffs the server installs (skillitemuse.go admits each one).

Both owners of a shop tab read the layout from here: the NPC roster
(simulation, the conversation's tab rows) and the shop catalog
(commerce, the offers behind a tab id). They cannot import each other,
so each calls ShopLayout on the same textdata and gets the same ids.
The ids sit above every authored tab and group, after the three tabs
and one group Jangan's alchemy shop synthesizes (alchemyshop.go), so the
two features never collide whether that one is on or off.

===========================================================================
*/

package pvpserver

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"opensro.online/server/internal/data/texttable"
)

// MaxGroupTabs is the most tabs one store group may hold: the client
// refuses a fifth (client-next merchant-branches.ts).
const MaxGroupTabs = 4

// The ids Jangan's alchemy shop takes above the authored maximum.
const (
	janganReservedTabs   int32 = 3
	janganReservedGroups int32 = 1
)

// itemdata columns: service flag, codename, the type triple.
const (
	itemColService  = 0
	itemColCodename = 2
	itemColTypeID1  = 9
	itemColTypeID2  = 10
	itemColTypeID3  = 11
	itemColumns     = 12
)

// refshoptab / refshoptabgroup: the id column.
const shopColID = 2

// The type triple's equipment and alchemy-stone families.
const (
	typeItem      = 3
	typeEquipment = 1
	typeEtc       = 3
	typeAlchemy   = 11
)

/*
================
ShopItem

One serviced itemdata row: its codename and type triple.
================
*/
type ShopItem struct {
	Codename                  string
	TypeID1, TypeID2, TypeID3 int
}

/*
================
ShopTab

One synthesized tab: the merchant it stands on, its ids and labels, the
goods in slot order and the plus they are sold at.
================
*/
type ShopTab struct {
	Npc        string
	TabID      int32
	GroupID    int32
	Label      string
	GroupLabel string
	Codenames  []string
	Plus       uint8
}

/*
================
shopCategory

A merchant's tab kind: the merchant, the tab label (its groups carry the
same), the plus and which items it takes. The order here is the tab order.
================
*/
type shopCategory struct {
	npc, label string
	plus       uint8
	takes      func(item ShopItem) bool
}

func gear(kinds ...int) func(ShopItem) bool {
	return func(item ShopItem) bool {
		return item.TypeID1 == typeItem && item.TypeID2 == typeEquipment &&
			slices.Contains(kinds, item.TypeID3) && pvpGrade(item.Codename)
	}
}

// potionGoods are the top grade of each recovery family: HP, MP and
// vigor potions with their special grades, and both cure pills.
var potionGoods = []string{
	"ITEM_ETC_HP_POTION_05", "ITEM_ETC_MP_POTION_05", "ITEM_ETC_ALL_POTION_05",
	"ITEM_ETC_HP_SPOTION_01", "ITEM_ETC_MP_SPOTION_01", "ITEM_ETC_ALL_SPOTION_01",
	"ITEM_ETC_CURE_ALL_04", "ITEM_ETC_CURE_RANDOM_04",
}

// buffGoods are the strongest timed buff of each kind among the shipped
// items whose skill the server's item use installs: damage, absorption,
// max HP and MP, hit, parry, agility and movement speed. The weaker
// grades (HP/MP +500) and the two-minute arena power-ups are left out.
var buffGoods = []string{
	"ITEM_MALL_DAMAGE_INC_20P_SCROLL", "ITEM_MALL_DAMAGE_ABS_20P_SCROLL",
	"ITEM_MALL_HP_INC_4100_POTION", "ITEM_MALL_MP_INC_4100_POTION",
	"ITEM_ETC_E051123_HIT_SCROLL", "ITEM_ETC_E051123_EVATION_SCROLL",
	"ITEM_ETC_E051123_AGILITY_SCROLL", "ITEM_MALL_MOVE_SPEED_UP_100",
}

func named(codenames []string) func(ShopItem) bool {
	return func(item ShopItem) bool { return slices.Contains(codenames, item.Codename) }
}

func stones(prefix string) func(ShopItem) bool {
	suffix := fmt.Sprintf("_%d", StoneDegree)
	return func(item ShopItem) bool {
		return item.TypeID1 == typeItem && item.TypeID2 == typeEtc && item.TypeID3 == typeAlchemy &&
			strings.HasPrefix(item.Codename, prefix) && strings.HasSuffix(item.Codename, suffix)
	}
}

var shopCategories = []shopCategory{
	{"NPC_CA_SMITH", "SN_TAB_WEAPON", ShopPlus, gear(6)},
	{"NPC_CA_SMITH", "SN_TAB_SHIELD", ShopPlus, gear(4)},
	{"NPC_CA_ARMOR", "SN_TAB_HEAVYARMOR_EU", ShopPlus, gear(3, 11)},
	{"NPC_CA_ARMOR", "SN_TAB_LIGHTARMOR_EU", ShopPlus, gear(2, 10)},
	{"NPC_CA_ARMOR", "SN_TAB_CLOTHES_EU", ShopPlus, gear(1, 9)},
	{"NPC_CA_ACCESSORY", "SN_TAB_ACCESSORY", ShopPlus, gear(5, 12)},
	{"NPC_CA_ACCESSORY", "UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE", 0,
		stones("ITEM_ETC_ARCHEMY_ATTRSTONE_")},
	{"NPC_CA_ACCESSORY", "UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE", 0,
		stones("ITEM_ETC_ARCHEMY_MAGICSTONE_")},
	{"NPC_CA_POTION", "SN_TAB_POTION", 0, named(potionGoods)},
	{"NPC_CA_POTION", "SN_TAB_SPECIAL", 0, named(buffGoods)},
}

/*
================
pvpGrade

The best gear for the level cap: degree 11 Nova (_A_RARE, including the
part-coded armour like _AA_A_RARE) and the two sealed sets (_SET_A_RARE,
_SET_B_RARE). The _ACTIVE and _HONOR variants end otherwise and stay out.
================
*/
func pvpGrade(codename string) bool {
	return strings.Contains(codename, "_11_") &&
		(strings.HasSuffix(codename, "_A_RARE") || strings.HasSuffix(codename, "_SET_B_RARE"))
}

/*
================
Layout

The tabs, in category order, each category's goods sorted and split into
tabs of TabCapacity. Each category opens a store group of its own, labelled
like its tabs, and a further group every MaxGroupTabs tabs, so no group
ever holds more tabs than the client takes.
================
*/
func Layout(items []ShopItem, maxTabID, maxGroupID int32) []ShopTab {
	nextTab := maxTabID + janganReservedTabs + 1
	nextGroup := maxGroupID + janganReservedGroups + 1
	var tabs []ShopTab
	for _, category := range shopCategories {
		var goods []string
		for _, item := range items {
			if category.takes(item) {
				goods = append(goods, item.Codename)
			}
		}
		if len(goods) == 0 {
			continue
		}
		slices.Sort(goods)
		var group int32
		inGroup := MaxGroupTabs
		for chunk := range slices.Chunk(goods, TabCapacity) {
			if inGroup == MaxGroupTabs {
				group, inGroup = nextGroup, 0
				nextGroup++
			}
			tabs = append(tabs, ShopTab{
				Npc: category.npc, TabID: nextTab, GroupID: group,
				Label: category.label, GroupLabel: category.label,
				Codenames: chunk, Plus: category.plus,
			})
			nextTab++
			inGroup++
		}
	}
	return tabs
}

/*
================
ShopLayout

Layout over the textdata directory: the serviced itemdata rows and the
largest authored tab and tab-group ids.
================
*/
func ShopLayout(dir string) ([]ShopTab, error) {
	items, err := readShopItems(dir)
	if err != nil {
		return nil, err
	}
	maxTab, err := maxID(filepath.Join(dir, "refshoptab.txt"))
	if err != nil {
		return nil, err
	}
	maxGroup, err := maxID(filepath.Join(dir, "refshoptabgroup.txt"))
	if err != nil {
		return nil, err
	}
	return Layout(items, maxTab, maxGroup), nil
}

/*
================
readShopItems

Every serviced row of the item shards itemdata.txt names. The manifest
spells names in mixed case; the shards are stored lower-case.
================
*/
func readShopItems(dir string) ([]ShopItem, error) {
	manifest, err := readTable(filepath.Join(dir, "itemdata.txt"))
	if err != nil {
		return nil, err
	}
	var items []ShopItem
	for _, entry := range manifest {
		if len(entry) == 0 || entry[0] == "" {
			continue
		}
		rows, err := readTable(filepath.Join(dir, strings.ToLower(entry[0])))
		if err != nil {
			return nil, err
		}
		for _, cols := range rows {
			if len(cols) < itemColumns || cols[itemColService] != "1" {
				continue
			}
			t1, _ := strconv.Atoi(cols[itemColTypeID1])
			t2, _ := strconv.Atoi(cols[itemColTypeID2])
			t3, _ := strconv.Atoi(cols[itemColTypeID3])
			items = append(items, ShopItem{Codename: cols[itemColCodename], TypeID1: t1, TypeID2: t2, TypeID3: t3})
		}
	}
	return items, nil
}

/*
================
maxID
================
*/
func maxID(path string) (int32, error) {
	rows, err := readTable(path)
	if err != nil {
		return 0, err
	}
	var highest int32
	for _, cols := range rows {
		if len(cols) <= shopColID {
			continue
		}
		if id, err := strconv.ParseInt(cols[shopColID], 10, 32); err == nil && int32(id) > highest {
			highest = int32(id)
		}
	}
	return highest, nil
}

/*
================
readTable

A textdata table as rows of cells: UTF-16LE with a byte-order mark or
UTF-8, comment and blank lines skipped (the roster's reader, npcworlddata.go).
================
*/
func readTable(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		units := make([]uint16, 0, (len(data)-2)/2)
		for index := 2; index+1 < len(data); index += 2 {
			units = append(units, binary.LittleEndian.Uint16(data[index:index+2]))
		}
		text = string(utf16.Decode(units))
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	cells := make(texttable.Cells)
	var rows [][]string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows, nil
}
