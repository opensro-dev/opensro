/*
===========================================================================

magiccube.go - Magic Cube mall listings (port-only, #3906)

Sixteen ITEM_PRE_MALL_MAGIC_CUBE_* rows the build stages behind
SRO_MAGIC_CUBE (scripts/magic_cube_import.py). Off (the default) is the
native game: the items are not in the data and nothing lists them. On,
the normal Silk Mall's etc tab sells each cube at the later client's
package silk (refpricepolicyofitem: 6 / 12 / 18 / 30 by capacity). No
Premium Mall tab: the owner's rule puts new goods on the shipped mall.

sro-gameworld wiring_magiccube.go turns the listings on only while the
switch is on and the build staged the rows.

===========================================================================
*/
package commerce

// magicCubeMallTab is a shipped mall etc tab (refshoptab MALL_CONSUME_ETC).
const magicCubeMallTab = "MALL_CONSUME_ETC"

/*
================
MagicCubeListing

One cube's mall package: the PRE_MALL item the import stages, the later
client's PACKAGE_ITEM_MALL_MAGIC_CUBE_* name, and its silk price.
================
*/
type MagicCubeListing struct {
	Codename    string
	MallPackage string
	Silk        uint32
}

// MagicCubeListings is every cube the feature sells, in the later client's
// display order (50-slot, then 10 / 20 / 30). Silk is the later package
// price (OriDB / later refpricepolicyofitem): 30 for 50 slots, 6 / 12 / 18
// for the sized cubes. Itemdata sellPrice is 0 on these cash rows, so the
// package table is the price source (Lucky Powder's gold shop uses
// itemdata; the cube's silk shop uses its package).
var MagicCubeListings = []MagicCubeListing{
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_WEAPON", Silk: 30},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ARMOR", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ARMOR", Silk: 30},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ACCESSARY", Silk: 30},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_SHIELD", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_SHIELD", Silk: 30},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_WEAPON_10", Silk: 6},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_10", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ARMOR_10", Silk: 6},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_10", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ACCESSARY_10", Silk: 6},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_10", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_SHIELD_10", Silk: 6},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_20", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_WEAPON_20", Silk: 12},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_20", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ARMOR_20", Silk: 12},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_20", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ACCESSARY_20", Silk: 12},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_20", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_SHIELD_20", Silk: 12},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_30", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_WEAPON_30", Silk: 18},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ARMOR_30", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ARMOR_30", Silk: 18},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_30", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_ACCESSARY_30", Silk: 18},
	{Codename: "ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_30", MallPackage: "PACKAGE_ITEM_MALL_MAGIC_CUBE_SHIELD_30", Silk: 18},
}

/*
================
MagicCubeMallGoods

The cubes as authored Item Mall packages on the etc tab, appended to
SRO_MALL_EXTRA_GOODS's listings when enabled is true.
================
*/
func MagicCubeMallGoods(enabled bool) []MallExtraGood {
	if !enabled {
		return nil
	}
	goods := make([]MallExtraGood, 0, len(MagicCubeListings))
	for _, cube := range MagicCubeListings {
		goods = append(goods, MallExtraGood{
			Tab: magicCubeMallTab, Package: cube.MallPackage,
			Item: cube.Codename, Silk: cube.Silk,
		})
	}
	return goods
}
