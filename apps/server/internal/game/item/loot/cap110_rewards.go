/*
===========================================================================
cap110_rewards.go - optional database-authored monster rewards.

Port-only, not native. The loot catalogue treats the native v1.150 client as
its content boundary, so the reward rules of the Alexandria and Pharaoh Tomb
monsters (fixed drops, random groups) were never carried. cap110-rewards.txt
(scripts/cap110_rewards.py) adds them. They join the assigned-drop tables of
consumables.go; the native rules of native monsters are never replaced.
===========================================================================
*/
package loot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const cap110RewardsFile = "cap110-rewards.txt"
const cap110RewardsVersion = 1

// nativeConsumables is the catalogue as the generator compiled it. InitCap110
// always joins onto it, so a second call replaces the first instead of
// stacking on it.
var nativeConsumables consumableCatalog

/*
================
cap110Rewards
================
*/
type cap110Rewards struct {
	Version int
	Fixed   []assignedDrop
	Random  []assignedRandom
	Groups  map[int][]groupDrop
}

/*
================
compileCap110Rewards

Joins the optional rules onto a copy of the native assigned-drop tables. A
rule that names a monster or a group the native tables already own refuses:
the overlay adds rewards for new monsters, it does not change native ones.
================
*/
func compileCap110Rewards(base consumableCatalog, data []byte) (consumableCatalog, error) {
	var source cap110Rewards
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return consumableCatalog{}, fmt.Errorf("cap110 rewards: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || source.Version != cap110RewardsVersion {
		return consumableCatalog{}, fmt.Errorf("cap110 rewards have invalid version or trailing data")
	}
	if len(source.Fixed) == 0 && len(source.Random) == 0 {
		return consumableCatalog{}, fmt.Errorf("cap110 rewards carry no rules")
	}
	next := consumableCatalog{families: base.families,
		fixed:  make(map[string][]assignedDrop, len(base.fixed)+len(source.Fixed)),
		random: make(map[string][]assignedRandom, len(base.random)+len(source.Random)),
		groups: make(map[int][]groupDrop, len(base.groups)+len(source.Groups))}
	for monster, rules := range base.fixed {
		next.fixed[monster] = rules
	}
	for monster, rules := range base.random {
		next.random[monster] = rules
	}
	for group, members := range base.groups {
		next.groups[group] = members
	}
	for group, members := range source.Groups {
		if _, owned := base.groups[group]; owned {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards reuse native group %d", group)
		}
		if len(members) == 0 {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards group %d is empty", group)
		}
		for _, member := range members {
			if !strings.HasPrefix(member.Codename, "ITEM_") || !chanceValid(member.Probability) {
				return consumableCatalog{}, fmt.Errorf("cap110 rewards group %d has an invalid member", group)
			}
		}
		next.groups[group] = members
	}
	seen := make(map[[2]string]bool, len(source.Fixed))
	for _, rule := range source.Fixed {
		key := [2]string{rule.Monster, rule.Item}
		if !strings.HasPrefix(rule.Monster, "MOB_") || !strings.HasPrefix(rule.Item, "ITEM_") || seen[key] ||
			!chanceValid(rule.Probability) || rule.Min > rule.Max {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards have an invalid fixed rule %s/%s", rule.Monster, rule.Item)
		}
		if len(base.fixed[rule.Monster]) != 0 || len(base.random[rule.Monster]) != 0 {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards redefine native monster %s", rule.Monster)
		}
		seen[key] = true
		next.fixed[rule.Monster] = append(next.fixed[rule.Monster], rule)
	}
	for _, rule := range source.Random {
		if !strings.HasPrefix(rule.Monster, "MOB_") || !chanceValid(rule.Probability) || rule.Min > rule.Max {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards have an invalid random rule %s/%d", rule.Monster, rule.Group)
		}
		if _, ok := source.Groups[rule.Group]; !ok {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards name an undefined group %d", rule.Group)
		}
		if len(base.fixed[rule.Monster]) != 0 || len(base.random[rule.Monster]) != 0 {
			return consumableCatalog{}, fmt.Errorf("cap110 rewards redefine native monster %s", rule.Monster)
		}
		next.random[rule.Monster] = append(next.random[rule.Monster], rule)
	}
	return next, nil
}

/*
================
chanceValid

A reward chance is a probability in (0, 1].
================
*/
func chanceValid(p float32) bool { return p > 0 && p <= 1 }
