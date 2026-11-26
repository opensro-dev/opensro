/*
===========================================================================

magiccube.go - Magic Cube switch, capacity and category filter

Port-only, not native (#3906). v1.150 has no Magic Cube. The later
client's sixteen mall rows (ITEM_PRE_MALL_MAGIC_CUBE_*) are TID 3/2/3/1 with
Param9 capacity (10/20/30/50) and Param10 category filter (1 weapon /
2 armor / 4 accessory / 8 shield). SRO_MAGIC_CUBE is also the build
switch that imports the rows (scripts/magic_cube_import.py). Off, the
server refuses deposit/withdraw and the sell refusal never fires on an
empty native bag.

===========================================================================
*/
package magiccube

import (
	"strings"

	"opensro.online/server/internal/game/item/wire"
)

// Elixir codename markers that map onto Param10 filter bits. The eight
// reinforce recipes (ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_{WEAPON,ARMOR,
// ACCESSARY,SHIELD}_{A,B}) are the only rows a cube admits; TID 3/3/10/1
// alone cannot tell the category apart.

const (
	// Env is the port-only switch; absent and unknown values stay native.
	Env = "SRO_MAGIC_CUBE"

	// Filter bits authored on Param10 of the cube row.
	FilterWeapon    = 1
	FilterArmor     = 2
	FilterAccessary = 4
	FilterShield    = 8

	// Param keys in ItemRef.NativeFields (enterworld/itemrefs.go).
	paramCapacity = "itemParam9_2bc"
	paramFilter   = "itemParam10_2c0"
)

/*
================
Enabled - port-only, not native; absent and unknown values stay native.
================
*/
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on":
		return true
	}
	return false
}

/*
================
IsCube - the item is a Magic Cube (TID 3.2.3).
================
*/
func IsCube(typeFlags uint16) bool {
	return wire.IsMagicCube(typeFlags)
}

/*
================
Capacity

The authored slot count (Param9 / itemParam9_2bc). Zero when the value
is not one the later client authors (10/20/30/50).
================
*/
func Capacity(param9 float64) int {
	n := int(param9)
	switch n {
	case 10, 20, 30, 50:
		return n
	}
	return 0
}

/*
================
Filter

The authored category bit (Param10 / itemParam10_2c0). Zero when the
value is not a later-client filter.
================
*/
func Filter(param10 float64) int {
	n := int(param10)
	switch n {
	case FilterWeapon, FilterArmor, FilterAccessary, FilterShield:
		return n
	}
	return 0
}

// ParamCapacityKey and ParamFilterKey are the NativeFields names
// enterworld builds for Param9 and Param10.
const (
	ParamCapacityKey = paramCapacity
	ParamFilterKey   = paramFilter
)

/*
================
NonEmpty

Whether the cube's StoredItemCount (InventoryRow.Data / ItemBody.Data)
says it still holds elixirs. A sell or consignment of a non-empty cube
is refused (UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR / 01:8E).
================
*/
func NonEmpty(data uint32) bool {
	return data > 0
}

/*
================
ElixirFilter

The filter bit an elixir's codename names. Zero when the row is not one
of the four reinforce categories a Magic Cube accepts.
================
*/
func ElixirFilter(codename string) int {
	switch {
	case strings.Contains(codename, "REINFORCE_RECIPE_WEAPON"):
		return FilterWeapon
	case strings.Contains(codename, "REINFORCE_RECIPE_ARMOR"):
		return FilterArmor
	case strings.Contains(codename, "REINFORCE_RECIPE_ACCESSARY"):
		return FilterAccessary
	case strings.Contains(codename, "REINFORCE_RECIPE_SHIELD"):
		return FilterShield
	}
	return 0
}

/*
================
MatchesFilter

Whether the elixir belongs in a cube authored with the given Param10 bit.
================
*/
func MatchesFilter(cubeFilter int, elixirCodename string) bool {
	want := Filter(float64(cubeFilter))
	got := ElixirFilter(elixirCodename)
	return want != 0 && got == want
}
