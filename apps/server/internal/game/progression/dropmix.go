/*
===========================================================================

dropmix.go - the beta drop rate per item family (port-only, not native)

The beta drop rate (growth.go) multiplies every drop pass of a kill alike,
so potions, pills, scrolls and arrows come twenty times over as well as the
equipment testers want, and the per-kill cap then spends its slots on them.
The drop mix gives named families their own rate: a family the mix names
runs that many passes, every other family keeps the beta rate. The owner
approved it on for the beta (2026-10-11); "off" restores the one uniform
beta rate, and with beta growth off nothing here applies.

Families, in the planner's terms (action.planMonsterKillLoot):

  - equipment: the ordinary equipment roll of each pass
  - assigned:  the monster's own assigned rewards (one round per rate)
  - potion, cure, scroll, elixir, ammo, stone, tablet: the seven
    consumable categories (loot.ConsumableFamilies), where ammo is arrows
    or bolts and stone is magic or attribute stones

Gold heaps keep their native count either way (growth.go).

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// EnvBetaDropMix names the setting: "on", "off" or "family=rate,...".
const EnvBetaDropMix = "SRO_BETA_DROP_MIX"

// DropMixFamilies are the names the setting accepts.
var DropMixFamilies = []string{"equipment", "assigned", "potion", "cure", "scroll", "elixir", "ammo", "stone", "tablet"}

// betaDropMixDefault is what "on" means: every consumable family at its
// native rate, equipment and assigned rewards at the beta rate (owner,
// 2026-10-11).
const betaDropMixDefault = "potion=1,cure=1,scroll=1,elixir=1,ammo=1,stone=1,tablet=1"

/*
================
DropMix

Family name to its drop rate. Nil is the uniform beta rate.
================
*/
type DropMix map[string]int

/*
================
ParseDropMix

"" and "off" are uniform; "on" is betaDropMixDefault; otherwise a list of
family=rate with rate 1..maxBetaDropRate. A malformed value is an error so
a typo never ships a different mix silently.
================
*/
func ParseDropMix(text string) (DropMix, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return nil, nil
	case "on", "1", "true":
		text = betaDropMixDefault
	}
	mix := DropMix{}
	for _, field := range strings.Split(text, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(field), "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if !found || !slices.Contains(DropMixFamilies, name) {
			return nil, fmt.Errorf("%s: %q is not family=rate (families: %s)", EnvBetaDropMix, field,
				strings.Join(DropMixFamilies, ", "))
		}
		if _, repeated := mix[name]; repeated {
			return nil, fmt.Errorf("%s: %s is named twice", EnvBetaDropMix, name)
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > maxBetaDropRate {
			return nil, fmt.Errorf("%s: %s rate %q is not 1..%d", EnvBetaDropMix, name, value, maxBetaDropRate)
		}
		mix[name] = n
	}
	return mix, nil
}

/*
================
BetaDropMixFromEnv
================
*/
func BetaDropMixFromEnv() (DropMix, error) {
	return ParseDropMix(os.Getenv(EnvBetaDropMix))
}

/*
================
DropMix.Rate

The family's rate: its own when the mix names it, else the uniform rate.
================
*/
func (mix DropMix) Rate(family string, uniform int) int {
	if rate, ok := mix[family]; ok {
		return rate
	}
	return uniform
}
