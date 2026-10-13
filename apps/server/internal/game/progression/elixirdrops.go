/*
===========================================================================

elixirdrops.go - elixirs only from the shop (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-13). With
SRO_ELIXIR_DROPS_OFF on, no monster drops an elixir: assigned drops, the
consumable mix's elixir family and a unique's recipe stage all pick
nothing where they would have picked one. The Jangan blacksmith sells all
eight at 1,000,000 gold (SRO_SHOP_EXTRA_GOODS, commerce/extragoods.go), so
that setting must list them while this one is on, or elixirs would have no
source at all. Quest rewards are not drops and are unchanged. Off, every
drop rolls exactly as before.

An elixir is the alchemy family of type IDs 3/3/10/1. v1.150's itemdata
holds exactly eight items of it: ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_
{WEAPON,SHIELD,ARMOR,ACCESSARY}_{A,B}.

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strings"
)

// EnvElixirDropsOff turns the rule on ("on", "1", "true").
const EnvElixirDropsOff = "SRO_ELIXIR_DROPS_OFF"

// elixirTypeIDs are an elixir's last three type IDs (the first is 3, an
// etc item).
var elixirTypeIDs = [3]int64{3, 10, 1}

// elixirCodenames are every item of elixirTypeIDs in v1.150's itemdata,
// the goods the shop must sell while drops are off.
var elixirCodenames = []string{
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B",
}

/*
================
ElixirDrops

The zero value is off: elixirs drop as native.
================
*/
type ElixirDrops struct {
	off bool
}

/*
================
ElixirDropsFromEnv
================
*/
func ElixirDropsFromEnv() ElixirDrops {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvElixirDropsOff))) {
	case "on", "1", "true":
		return ElixirDrops{off: true}
	}
	return ElixirDrops{}
}

/*
================
ElixirDrops.Off
================
*/
func (e ElixirDrops) Off() bool { return e.off }

/*
================
ElixirDrops.Excludes

Whether an item of these type IDs may no longer drop.
================
*/
func (e ElixirDrops) Excludes(typeIDs [4]int64) bool {
	return e.off && typeIDs[0] == 3 && typeIDs[1] == elixirTypeIDs[0] &&
		typeIDs[2] == elixirTypeIDs[1] && typeIDs[3] == elixirTypeIDs[2]
}

/*
================
ElixirDrops.CheckSource

With drops off, every elixir must be sold: sold reports whether the shop
lists a codename. An unsold elixir stops the boot naming both settings.
================
*/
func (e ElixirDrops) CheckSource(sold func(codename string) bool, shopSetting string) error {
	if !e.off {
		return nil
	}
	for _, codename := range elixirCodenames {
		if !sold(codename) {
			return fmt.Errorf("%s is on but %s does not sell %s: elixirs would have no source", EnvElixirDropsOff, shopSetting, codename)
		}
	}
	return nil
}
