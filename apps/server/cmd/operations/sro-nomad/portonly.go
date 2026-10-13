/*
===========================================================================

portonly.go - the GameWorld job's port-only switches

Each switch here turns on behaviour the original game does not have. The
deployer's value reaches the GameWorld job as written and the GameWorld
parses it; the GameWorld's own unset value is always native.

  - SRO_STORAGE_AUTO_STACK (storage_auto_stack): storage quick moves land
    on a matching stack (action/storageautostack.go). The deployment
    default is on, as the owner decided on 2026-10-11.
  - SRO_REVERSE_RETURN_MAP (reverse_return_map): the reverse return
    scroll's map destinations (action/reversemap.go). Off by default.
  - SRO_ALCHEMY_RATES (alchemy_rates): the reinforcement rate table
    (alchemy/rates.go). The deployment default is on, as the owner
    decided on 2026-10-11; "off" is the authored chances.
  - SRO_GM_SETSPEED (gm_setspeed): a GM's /SETSPEED (action/gmspeed.go).
    The deployment default is on, as the owner decided on 2026-10-12;
    "off" refuses the command, which v1.150 does not have.
  - SRO_ALCHEMY_MAX_PLUS (alchemy_max_plus): the enhancement cap
    (alchemy/maxplus.go). The deployment default is on (+11), as the owner
    decided on 2026-10-13; "off" is the original game's ceiling.
  - SRO_TEAM_DEATH_REVIVE (team_death_revive): whether a team death
    revives where it fell (action/teamdeath.go). Unlike the others, "on"
    is native and the deployment default (owner, 2026-10-13); "off" is the
    port-only town rule.
  - SRO_MALL_EXTRA_GOODS (mall_extra_goods): authored Item Mall packages
    the v1.150 mall does not list (commerce/mallextra.go). The deployment
    default is on, as the owner decided on 2026-10-13; "off" is the
    shipped mall.
  - SRO_ALCHEMY_NO_ASSIMILATION (alchemy_no_assimilation): stones without
    assimilation (alchemy/noassimilation.go). The deployment default is on,
    as the owner decided on 2026-10-13; "off" is the original game.
  - SRO_CARAVAN_BANDIT_CAP (caravan_bandit_cap): the bandits per caravan
    ambush (caravan/banditcap.go). The deployment default is 16, as the
    owner decided on 2026-10-13; "off" is the original game's uncapped count.

===========================================================================
*/
package main

import (
	"os"
	"strings"
)

// storageAutoStackDefault is the owner-approved deployment default.
const storageAutoStackDefault = "on"

// alchemyRatesDefault is the owner-approved deployment default.
const alchemyRatesDefault = "on"

// gmSetSpeedDefault is the owner-approved deployment default.
const gmSetSpeedDefault = "on"

// alchemyMaxPlusDefault is the owner-approved deployment default.
const alchemyMaxPlusDefault = "on"

// teamDeathReviveDefault is native, the owner's deployment default.
const teamDeathReviveDefault = "on"

// mallExtraGoodsDefault is the owner-approved deployment default.
const mallExtraGoodsDefault = "on"

// alchemyNoAssimilationDefault is the owner-approved deployment default.
const alchemyNoAssimilationDefault = "on"

// caravanBanditCapDefault is the owner-approved deployment default.
const caravanBanditCapDefault = "16"

/*
================
portOnlySettings
================
*/
type portOnlySettings struct {
	StorageAutoStack string
	ReverseReturnMap string
	AlchemyRates     string
	GMSetSpeed       string
	AlchemyMaxPlus   string
	TeamDeathRevive  string
	MallExtraGoods   string
	// AlchemyNoAssimilation is SRO_ALCHEMY_NO_ASSIMILATION.
	AlchemyNoAssimilation string
	// CaravanBanditCap is SRO_CARAVAN_BANDIT_CAP.
	CaravanBanditCap string
}

/*
================
portOnlySettingsFromEnv

The deployer's values as written; storage auto-stack, the alchemy rates,
/SETSPEED, the enhancement cap, the team-death revive, the mall goods, stone
assimilation and the bandit cap take their defaults when unset.
================
*/
func portOnlySettingsFromEnv() portOnlySettings {
	settings := portOnlySettings{
		StorageAutoStack:      strings.TrimSpace(os.Getenv("SRO_STORAGE_AUTO_STACK")),
		ReverseReturnMap:      strings.TrimSpace(os.Getenv("SRO_REVERSE_RETURN_MAP")),
		AlchemyRates:          strings.TrimSpace(os.Getenv("SRO_ALCHEMY_RATES")),
		GMSetSpeed:            strings.TrimSpace(os.Getenv("SRO_GM_SETSPEED")),
		AlchemyMaxPlus:        strings.TrimSpace(os.Getenv("SRO_ALCHEMY_MAX_PLUS")),
		TeamDeathRevive:       strings.TrimSpace(os.Getenv("SRO_TEAM_DEATH_REVIVE")),
		MallExtraGoods:        strings.TrimSpace(os.Getenv("SRO_MALL_EXTRA_GOODS")),
		AlchemyNoAssimilation: strings.TrimSpace(os.Getenv("SRO_ALCHEMY_NO_ASSIMILATION")),
		CaravanBanditCap:      strings.TrimSpace(os.Getenv("SRO_CARAVAN_BANDIT_CAP")),
	}
	if settings.StorageAutoStack == "" {
		settings.StorageAutoStack = storageAutoStackDefault
	}
	if settings.AlchemyRates == "" {
		settings.AlchemyRates = alchemyRatesDefault
	}
	if settings.GMSetSpeed == "" {
		settings.GMSetSpeed = gmSetSpeedDefault
	}
	if settings.AlchemyMaxPlus == "" {
		settings.AlchemyMaxPlus = alchemyMaxPlusDefault
	}
	if settings.TeamDeathRevive == "" {
		settings.TeamDeathRevive = teamDeathReviveDefault
	}
	if settings.MallExtraGoods == "" {
		settings.MallExtraGoods = mallExtraGoodsDefault
	}
	if settings.AlchemyNoAssimilation == "" {
		settings.AlchemyNoAssimilation = alchemyNoAssimilationDefault
	}
	if settings.CaravanBanditCap == "" {
		settings.CaravanBanditCap = caravanBanditCapDefault
	}
	return settings
}

/*
================
jobVariables

Adds the switches to the GameWorld job's variables.
================
*/
func (settings portOnlySettings) jobVariables(variables map[string]any) map[string]any {
	variables["storage_auto_stack"] = settings.StorageAutoStack
	variables["reverse_return_map"] = settings.ReverseReturnMap
	variables["alchemy_rates"] = settings.AlchemyRates
	variables["gm_setspeed"] = settings.GMSetSpeed
	variables["alchemy_max_plus"] = settings.AlchemyMaxPlus
	variables["team_death_revive"] = settings.TeamDeathRevive
	variables["mall_extra_goods"] = settings.MallExtraGoods
	variables["alchemy_no_assimilation"] = settings.AlchemyNoAssimilation
	variables["caravan_bandit_cap"] = settings.CaravanBanditCap
	return variables
}
