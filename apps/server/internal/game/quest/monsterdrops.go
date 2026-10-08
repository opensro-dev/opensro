/*
===========================================================================

monsterdrops.go - native personal quest loot admission

Fatal hits plan ground loot; only pickup changes inventory objectives. A
material quest may drop a tool rather than its final collection item, so the
drop contract owns its item identity and held-item cap independently.

===========================================================================
*/
package quest

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
MonsterDropRule

An empty ItemCodename retains the ordinary collected-item drop contract.
================
*/
type MonsterDropRule struct {
	ItemCodename     string
	MonsterCodenames []string
	AnyMonster       bool
	ChancePercent    float32
	// Optional rates aligned with MonsterCodenames; exclusive with the shared
	// rate. Lua stores each species' probability as float32 (86c077..86c081).
	SpeciesChancePercent []float32
	// MonsterGrades, when set, is aligned with MonsterCodenames: a kill counts
	// only when the monster's grade (rarity & 15, CGObjMob_GetBaseGrade
	// 4C1C60) matches. Gather missions set it with +0x10D and list the
	// grades at +0x111 (91C4D0).
	MonsterGrades  []uint8
	MinPlayerLevel uint8
	MaxHeld        uint32
	// DropCount is the stack one successful drop leaves (mission +0x245,
	// read by 91C660); zero means one.
	DropCount uint16
}

/*
================
validateMonsterDrop

Reject malformed probability and cap contracts before loading the catalog.
================
*/
func validateMonsterDrop(spec QuestSpec) error {
	rule := spec.MonsterDrop
	if rule == nil {
		return nil
	}
	validChance := func(p float32) bool { return p > 0 && p <= 100 && !math.IsNaN(float64(p)) }
	if spec.Objective != ObjectiveCollect ||
		rule.AnyMonster == (len(rule.MonsterCodenames) != 0) ||
		(rule.ItemCodename == "" && rule.MaxHeld != 0 && rule.MaxHeld < spec.CollectCount) {
		return fmt.Errorf("quest definitions: %s has an invalid monster-drop contract", spec.Codename)
	}
	if len(rule.SpeciesChancePercent) == 0 {
		if !validChance(rule.ChancePercent) {
			return fmt.Errorf("quest %s invalid drop probability", spec.Codename)
		}
	} else {
		if rule.AnyMonster || rule.ChancePercent != 0 || len(rule.SpeciesChancePercent) != len(rule.MonsterCodenames) {
			return fmt.Errorf("quest %s invalid species rates", spec.Codename)
		}
		for _, p := range rule.SpeciesChancePercent {
			if !validChance(p) {
				return fmt.Errorf("quest %s invalid species probability", spec.Codename)
			}
		}
	}
	if len(rule.MonsterGrades) > 0 && (rule.AnyMonster || len(rule.MonsterGrades) != len(rule.MonsterCodenames)) {
		return fmt.Errorf("quest %s invalid monster grades", spec.Codename)
	}
	seen := make(map[string]bool)
	for _, code := range rule.MonsterCodenames {
		if !strings.HasPrefix(code, "MOB_") || seen[code] {
			return fmt.Errorf("quest definitions: %s has an invalid or duplicate monster target %q", spec.Codename, code)
		}
		seen[code] = true
	}
	return nil
}

/*
================
MonsterDrops

The accepted fatal-hit owner commits these personal drops with ordinary loot.
Random draws never mutate quest state or award inventory directly. rarity is
the killed monster's rarity byte.
================
*/
func (rt *Runtime) MonsterDrops(c *enterworld.Character, monster string, rarity uint8, roll func() (uint32, error)) []inventory.ItemAmount {
	if c == nil || c.DeletePending || roll == nil {
		return nil
	}
	var out []inventory.ItemAmount
	for _, record := range c.ActiveQuests {
		def, ok := rt.Defs.ByRefID(record.RefID)
		if ok {
			def, ok = definitionAtStage(def, record.Stage)
		}
		if !ok || def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 || waitingBranch(def, record) {
			continue
		}
		for i := 0; i < missionCount(def); i++ {
			m := missionDefinition(def, i)
			out = append(out, missionMonsterDrops(c, m, monster, rarity, roll)...)
		}
	}
	return out
}

/*
================
missionMonsterDrops

Held-item admission and drop identity share the same codename. A knife drop
must not stop because a different stack of collected vines reached its cap.
================
*/
func missionMonsterDrops(c *enterworld.Character, def *Definition, monster string, rarity uint8, roll func() (uint32, error)) []inventory.ItemAmount {
	if def.Objective != ObjectiveCollect || def.MonsterDrop == nil {
		return nil
	}
	rule := def.MonsterDrop
	code := rule.ItemCodename
	if code == "" {
		code = def.CollectItemCodename
	}
	if !rule.AnyMonster && !slices.Contains(rule.MonsterCodenames, monster) {
		return nil
	}
	if len(rule.MonsterGrades) > 0 && rule.MonsterGrades[slices.Index(rule.MonsterCodenames, monster)] != rarity&15 {
		return nil
	}
	if c.Level == nil || *c.Level < int64(rule.MinPlayerLevel) {
		return nil
	}
	var held uint64
	for _, item := range c.MissionInventory {
		matches := item.RefObjID == def.CollectItemRefID
		if rule.ItemCodename != "" {
			matches = item.Codename == code
		}
		if matches && inventory.InBag(c, item.Slot) {
			held += uint64(max(1, item.StackCount))
		}
	}
	cap := rule.MaxHeld
	if cap == 0 {
		cap = def.CollectCount
	}
	if held >= uint64(cap) {
		return nil
	}
	chance := rule.ChancePercent
	if len(rule.SpeciesChancePercent) > 0 {
		chance = rule.SpeciesChancePercent[slices.Index(rule.MonsterCodenames, monster)]
	}
	first, err := roll()
	if err != nil {
		return nil
	}
	second, err := roll()
	if err != nil || !nativeQuestDropChance(chance, first, second) {
		return nil
	}
	count := uint32(max(1, rule.DropCount))
	return []inventory.ItemAmount{{Codename: code, Count: count}}
}

/*
================
nativeQuestDropChance

57BE70 joins two 15-bit draws and rounds the normalized sample to float32
before comparison. Zero fails and equality succeeds, including fractional rates.
================
*/
func nativeQuestDropChance(chance float32, first, second uint32) bool {
	value := ((second&0x7fff)<<15 | (first & 0x7fff)) % 1000000
	sample := float32(float64(value) / 1000000)
	return sample >= float32(0.000001) && float64(chance) >= float64(sample)*100
}
