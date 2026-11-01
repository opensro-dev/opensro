/*
===========================================================================

rates.go - the shard's reinforcement rate table (port-only, not native)

Natively an elixir's chance at each plus is authored in its own parameters
(the reference server's probability field): a B-grade elixir gives 50% for
+1, falling to 12%
from +10, and a matching lucky powder adds its own authored chance. Players
asked for a gentler curve, and the owner approved it as a server setting
(2026-10-11) that returns to native with one value.

SRO_ALCHEMY_RATES replaces the elixir's authored chance with one table
indexed by the item's current plus: "on" is the requested table
(+1 100% ... +11 5%), a comma list such as "100,80,70" is a custom one
whose last entry holds for every higher plus (as native +11 does), and
unset or "off" is native. The table holds for every elixir, so A and B
grade elixirs give the same chance while it is set (natively A is about
half of B). Everything else around the chance is unchanged:
a lucky powder, a Luck charge and the premium bonuses still add, the item
still breaks or curses from +5, and Immortal, Steady and Astral still
protect it. Only the native 10% floor is dropped while a table is set: the
table's own entries are the chances the operator chose (+11 is 5%).

===========================================================================
*/

package alchemy

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvReinforceRates names the setting: "on", a comma list, or "off".
const EnvReinforceRates = "SRO_ALCHEMY_RATES"

// maxReinforceRates bounds a custom table; a plus byte never needs more.
const maxReinforceRates = 32

// requestedReinforceRates is the table "on" selects, the chance of each
// next plus from +1 to +11 (owner decision 2026-10-11).
var requestedReinforceRates = ReinforceRates{100, 80, 70, 60, 50, 40, 30, 20, 15, 10, 5}

/*
================
ReinforceRates

The chance in percent of reaching plus+1, indexed by the current plus; the
last entry holds above the table. Nil is native.
================
*/
type ReinforceRates []int

/*
================
ParseReinforceRates

"" and "off" are native; "on" is the requested table; anything else must
be a list of 1..maxReinforceRates percentages in 0..100. A malformed value
is an error so a typo never ships the wrong odds silently.
================
*/
func ParseReinforceRates(text string) (ReinforceRates, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return nil, nil
	case "on", "1", "true":
		return append(ReinforceRates(nil), requestedReinforceRates...), nil
	}
	fields := strings.Split(text, ",")
	if len(fields) > maxReinforceRates {
		return nil, fmt.Errorf("%s: %d entries, at most %d", EnvReinforceRates, len(fields), maxReinforceRates)
	}
	rates := make(ReinforceRates, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || n < 0 || n > ReinforceMaxChance {
			return nil, fmt.Errorf("%s: %q is not a percentage", EnvReinforceRates, field)
		}
		rates = append(rates, n)
	}
	return rates, nil
}

/*
================
ReinforceRatesFromEnv
================
*/
func ReinforceRatesFromEnv() (ReinforceRates, error) {
	return ParseReinforceRates(os.Getenv(EnvReinforceRates))
}

/*
================
ReinforceRates.chance
================
*/
func (rates ReinforceRates) chance(plus uint8) int {
	i := int(plus)
	if i >= len(rates) {
		i = len(rates) - 1
	}
	return rates[i]
}
