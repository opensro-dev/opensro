/*
===========================================================================

mallextra.go - Item Mall goods beyond the v1.150 mall list

Port-only, not native, beta balance (owner 2026-10-13). v1.150 ships avatar
outfits whose packages, scraps and icons are all authored but which its mall
never lists: fur, Halloween (with its attachment), pirate and holy knight,
and the angel and devil wing dresses. The operator lists them with one
setting instead of a data edit. "off" is the shipped mall exactly.

An entry is "mall tab : package codename". Each package keeps its own
authored price (refpricepolicyofitem already prices every one of them: the
dresses 130 silk and the hats 30, the European wedding set's tier, and the
Halloween attachment 15). Each entry becomes the goods row its tab would
carry, at the tab's next free slot, before the catalogue is built, so every
check the shipped mall passes (one package per code, a price, scraps, no
unsupported policy, unique slots) also holds for it. A malformed entry, an
unknown tab, a package without data or one the mall already lists stops the
boot naming the setting.

An authored entry, "tab : package : item : silk", lists an item v1.150 has
no mall package for: the package row (its name, description and icon are
the item's own), one plain scrap and a silk price are written as the
shipped data would carry them, under a package ID past the shipped ones.
The list may name presets: "on" (the avatar outfits) and "statrecall" (the
stat point recall scroll's six level tiers, owner 2026-10-25, #1609), so
"on,statrecall" adds the scrolls to today's mall and "on" alone is today's
mall exactly.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvMallExtraGoods names the setting: "on", "off" or "tab:package,...".
const EnvMallExtraGoods = "SRO_MALL_EXTRA_GOODS"

// mallExtraGoodsDefault is what "on" means (owner, 2026-10-13): every v1.150
// avatar outfit the mall does not list, male and female. v1.150 has no
// Chinese wedding set (itemdata holds the European one only, already sold).
var mallExtraGoodsDefault = strings.Join([]string{
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_FUR",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_FUR",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_PIRATE",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_PIRATE",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_KNIGHT",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_KNIGHT",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_ANGEL_WING_DRESS",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_ANGEL_WING_DRESS",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_M_DEVIL_WING_DRESS",
	"MALL_AVATAR_DRESS:PACKAGE_ITEM_MALL_AVATAR_W_DEVIL_WING_DRESS",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_FUR_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_W_FUR_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_PIRATE_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_W_PIRATE_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_KNIGHT_HAT",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_W_KNIGHT_HAT",
	// v1.150 has no attachment tab; the hat tab holds the other head-and-
	// shoulder pieces.
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH",
	"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN_ATTACH",
}, ",")

// mallStatRecallPreset is "statrecall" (#1609): the recall scroll's six
// level tiers (itemdata 9253-9258) for silk, priced by tier (owner,
// 2026-10-26): 30/60/90/120/160 for 1-80, and one price, 250, for the top
// scroll, which the preset makes usable at 81-110 (StatRecallListed).
var mallStatRecallPreset = strings.Join([]string{
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL:ITEM_MALL_STATS_POINT_RECALL:30",
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL_50LV:ITEM_MALL_STATS_POINT_RECALL_50LV:60",
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL_60LV:ITEM_MALL_STATS_POINT_RECALL_60LV:90",
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL_70LV:ITEM_MALL_STATS_POINT_RECALL_70LV:120",
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL_80LV:ITEM_MALL_STATS_POINT_RECALL_80LV:160",
	"MALL_CONSUME_SCROLL:PACKAGE_ITEM_MALL_STATS_POINT_RECALL_81LV:ITEM_MALL_STATS_POINT_RECALL_81LV:250",
}, ",")

// maxAuthoredSilk bounds an authored price: the 1500-silk bank cap many
// times over, far below the wire's u32.
const maxAuthoredSilk = 100000

/*
================
MallExtraGood

One configured listing: the mall tab and the package it lists. Item and
Silk are set only for an authored package (one the shipped data lacks).
================
*/
type MallExtraGood struct {
	Tab     string
	Package string
	Item    string
	Silk    uint32
}

/*
================
ParseMallExtraGoods

"" and "off" are the shipped mall (nil); "on", "1" and "true" are
mallExtraGoodsDefault. Otherwise a comma list of tab:package listings,
tab:package:item:silk authored packages and the presets "on" and
"statrecall".
================
*/
func ParseMallExtraGoods(text string) ([]MallExtraGood, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return nil, nil
	case "1", "true":
		text = "on"
	}
	var fields []string
	for _, field := range strings.Split(text, ",") {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "on":
			fields = append(fields, strings.Split(mallExtraGoodsDefault, ",")...)
		case "statrecall":
			fields = append(fields, strings.Split(mallStatRecallPreset, ",")...)
		default:
			fields = append(fields, field)
		}
	}
	var goods []MallExtraGood
	for _, field := range fields {
		parts := strings.Split(strings.TrimSpace(field), ":")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
			if parts[i] == "" {
				return nil, fmt.Errorf("%s: %q has an empty field", EnvMallExtraGoods, field)
			}
		}
		switch len(parts) {
		case 2:
			goods = append(goods, MallExtraGood{Tab: parts[0], Package: parts[1]})
		case 4:
			silk, err := strconv.ParseUint(parts[3], 10, 32)
			if err != nil || silk == 0 || silk > maxAuthoredSilk {
				return nil, fmt.Errorf("%s: %q needs a silk price from 1 to %d", EnvMallExtraGoods, field, maxAuthoredSilk)
			}
			goods = append(goods, MallExtraGood{Tab: parts[0], Package: parts[1], Item: parts[2], Silk: uint32(silk)})
		default:
			return nil, fmt.Errorf("%s: %q is not tab:package or tab:package:item:silk", EnvMallExtraGoods, field)
		}
	}
	return goods, nil
}

/*
================
StatRecallListed

Whether goods sell the recall scrolls (the "statrecall" preset). Port-only,
not native: while they are sold, each scroll is usable only inside its
tier's level window, and the top one (9258) up to level 110 (action
itemuse.go statRecallWindow), so a cheap low tier never stands in for a
dear high one. v1.150 sells none, and with the preset off use is as today.
================
*/
func StatRecallListed(goods []MallExtraGood) bool {
	for _, good := range goods {
		if strings.HasPrefix(good.Item, "ITEM_MALL_STATS_POINT_RECALL") {
			return true
		}
	}
	return false
}

/*
================
MallExtraGoodsFromEnv
================
*/
func MallExtraGoodsFromEnv() ([]MallExtraGood, error) {
	return ParseMallExtraGoods(os.Getenv(EnvMallExtraGoods))
}

/*
================
addMallExtraRows

Writes each listing into the reference tables as the shipped data would
carry it: a goods row at the tab's next free slot. A plain listing's
package data (refpackageitem, its price policy, its scraps) must already be
authored; an authored entry writes them first (authorMallPackage). The
catalogue build checks the rest.
================
*/
func addMallExtraRows(tables map[string][][]string, goods []MallExtraGood, refs enterworld.ItemRefSource) error {
	if len(goods) == 0 {
		return nil
	}
	mallTab := map[string]bool{}
	for _, row := range tables["refshoptab"] {
		if len(row) >= 4 && strings.HasPrefix(row[3], "MALL_") {
			mallTab[row[3]] = true
		}
	}
	listed := map[string]bool{}
	next := map[string]int{}
	for _, row := range tables["refshopgoods"] {
		if len(row) < 5 || !mallTab[row[2]] {
			continue
		}
		listed[row[3]] = true
		if slot, err := strconv.Atoi(row[4]); err == nil && slot+1 > next[row[2]] {
			next[row[2]] = slot + 1
		}
	}
	authored := map[string]bool{}
	for _, row := range tables["refpackageitem"] {
		if len(row) >= 4 {
			authored[row[3]] = true
		}
	}
	nextPackageID := uint64(0)
	for _, row := range tables["refpackageitem"] {
		if len(row) >= 3 {
			if id, err := strconv.ParseUint(row[2], 10, 32); err == nil && id >= nextPackageID {
				nextPackageID = id + 1
			}
		}
	}
	for _, good := range goods {
		if good.Item != "" {
			if authored[good.Package] {
				return fmt.Errorf("%s: %s already has package data; list it as tab:package", EnvMallExtraGoods, good.Package)
			}
			if err := authorMallPackage(tables, good, refs, uint32(nextPackageID)); err != nil {
				return err
			}
			authored[good.Package] = true
			nextPackageID++
		}
		switch {
		case !mallTab[good.Tab]:
			return fmt.Errorf("%s: unknown mall tab %s", EnvMallExtraGoods, good.Tab)
		case !authored[good.Package]:
			return fmt.Errorf("%s: %s has no package data", EnvMallExtraGoods, good.Package)
		case listed[good.Package]:
			return fmt.Errorf("%s: %s is already in the mall", EnvMallExtraGoods, good.Package)
		case next[good.Tab] > math.MaxUint8:
			return fmt.Errorf("%s: %s is full", EnvMallExtraGoods, good.Tab)
		}
		listed[good.Package] = true
		tables["refshopgoods"] = append(tables["refshopgoods"],
			[]string{"1", "11", good.Tab, good.Package, strconv.Itoa(next[good.Tab])})
		next[good.Tab]++
	}
	return nil
}

/*
================
authorMallPackage

The rows v1.150 would carry for a one-item silk package it never shipped:
the package (named, described and drawn as the item itself), one plain
scrap of the item (extraTemplate: no plus, variance or options) and the
silk price. No gift-silk or point row: the authored package is silk only.
================
*/
func authorMallPackage(tables map[string][][]string, good MallExtraGood, refs enterworld.ItemRefSource, id uint32) error {
	if refs == nil {
		return fmt.Errorf("%s: %s needs the item table to author its package", EnvMallExtraGoods, good.Package)
	}
	ref, ok := refs.ItemRefByCodename(good.Item)
	if !ok || ref == nil {
		return fmt.Errorf("%s: %s names no item %s", EnvMallExtraGoods, good.Package, good.Item)
	}
	tables["refpackageitem"] = append(tables["refpackageitem"], []string{
		"1", "11", strconv.FormatUint(uint64(id), 10), good.Package, "0", "EXPAND_TERM_ALL",
		ref.NameStrID, ref.DescriptionSymbol, ref.Icon,
		"-1", "xxx", "-1", "xxx", "-1", "xxx", "-1", "xxx",
	})
	scrap := extraTemplate(good.Item)
	scrap[1], scrap[2] = "11", good.Package
	tables["refscrapofpackageitem"] = append(tables["refscrapofpackageitem"], scrap)
	tables["refpricepolicyofitem"] = append(tables["refpricepolicyofitem"], []string{
		"1", "11", good.Package, "2", strconv.FormatUint(uint64(good.Silk), 10),
		"0", "xxx", "0", "xxx", "0", "xxx", "0", "xxx",
	})
	return nil
}
