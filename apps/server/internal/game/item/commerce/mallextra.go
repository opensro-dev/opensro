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

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
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

/*
================
MallExtraGood

One configured listing: the mall tab and the package it lists.
================
*/
type MallExtraGood struct {
	Tab     string
	Package string
}

/*
================
ParseMallExtraGoods

"" and "off" are the shipped mall (nil); "on" is mallExtraGoodsDefault;
otherwise comma-separated tab:package entries.
================
*/
func ParseMallExtraGoods(text string) ([]MallExtraGood, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return nil, nil
	case "on", "1", "true":
		text = mallExtraGoodsDefault
	}
	var goods []MallExtraGood
	for _, field := range strings.Split(text, ",") {
		parts := strings.Split(strings.TrimSpace(field), ":")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("%s: %q is not tab:package", EnvMallExtraGoods, field)
		}
		goods = append(goods, MallExtraGood{Tab: strings.TrimSpace(parts[0]), Package: strings.TrimSpace(parts[1])})
	}
	return goods, nil
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
carry it: a goods row at the tab's next free slot. The package's own data
(refpackageitem, its price policy, its scraps) must already be authored;
the catalogue build checks the rest.
================
*/
func addMallExtraRows(tables map[string][][]string, goods []MallExtraGood) error {
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
	for _, good := range goods {
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
