/*
===========================================================================
wiring_caravanbuilds.go - port-only max-level caravan equipment and skills

References and requirements come from the shipped catalogues. Ordinary and
Sun actors use the same combat/stat pipeline as equipped player characters.
===========================================================================
*/
package main

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"sort"
	"strings"
)

/*
================
equipCaravanActor
================
*/
func (game *gameplayPlane) equipCaravanActor(c *enterworld.Character, unit *caravanai.AgentUnit, sun bool) error {
	degree := *c.Level / 10
	suffix := "_C"
	if sun {
		suffix += "_RARE"
	}
	gender := "M"
	if *c.Gender == 1 {
		gender = "W"
	}
	family := strings.Split(unit.WeaponCode, "_")[2]
	gear := []struct {
		slot int64
		code string
	}{{6, fmt.Sprintf("ITEM_CH_%s_%02d%s", family, degree, suffix)}, {8, unit.JobSuitCode}}
	for slot, part := range []string{"HA", "BA", "SA", "LA", "AA", "FA"} {
		gear = append(gear, struct {
			slot int64
			code string
		}{int64(slot), fmt.Sprintf("ITEM_CH_%s_HEAVY_%02d_%s%s", gender, degree, part, suffix)})
	}
	for slot, part := range []string{"EARRING", "NECKLACE", "RING", "RING"} {
		gear = append(gear, struct {
			slot int64
			code string
		}{int64(9 + slot), fmt.Sprintf("ITEM_CH_%s_%02d%s", part, degree, suffix)})
	}
	if family == "SWORD" || family == "BLADE" {
		gear = append(gear, struct {
			slot int64
			code string
		}{7, fmt.Sprintf("ITEM_CH_SHIELD_%02d%s", degree, suffix)})
	}
	for _, piece := range gear {
		ref, ok := game.deps.Items.ItemRefByCodename(piece.code)
		if !ok {
			return fmt.Errorf("caravan equipment %s unavailable", piece.code)
		}
		for i, kind := range ref.ReqQuadTypes {
			if kind == 1 && ref.ReqQuadValues[i] > *c.Level {
				return fmt.Errorf("caravan equipment %s requires level %d", piece.code, ref.ReqQuadValues[i])
			}
		}
		c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: piece.slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), VarianceBits: "0", Durability: max(ref.MaxDurability, 1), StackCount: 1})
	}
	return nil
}

/*
================
trainCaravanActor
================
*/
func (game *gameplayPlane) trainCaravanActor(c *enterworld.Character) error {
	source, ok := game.deps.Skills.(interface {
		SpawnSkillRows() []enterworld.SpawnSkillRow
	})
	if !ok {
		return fmt.Errorf("caravan skill catalogue unavailable")
	}
	weapon := c.MissionInventory[0].Codename
	family, mastery := "SWORD", uint32(257)
	if strings.Contains(weapon, "_SPEAR_") || strings.Contains(weapon, "_TBLADE_") {
		family, mastery = "SPEAR", 258
	}
	if strings.Contains(weapon, "_BOW_") {
		family, mastery = "BOW", 259
	}
	c.Masteries = domain.DefaultMasteries(enterworld.RaceKeyChina)
	for i := range c.Masteries {
		if c.Masteries[i].ID == mastery {
			c.Masteries[i].Level = *c.Level
		}
	}
	groups := map[uint32]enterworld.SkillRow{}
	for _, row := range source.SpawnSkillRows() {
		skill, found := game.deps.Skills.SkillByID(row.ID)
		if !found || !strings.HasPrefix(skill.Codename, "SKILL_CH_"+family+"_") || strings.Contains(skill.Codename, "_BASE_") || skill.ChainSub || skill.SPCost <= 0 || !skill.CombatPinned || !skill.Attack.Present || skill.OffenseRefusal != "" || skill.Reqc.KnockedDown || skill.PositionEffect.Pinned || !action.SkillPressHandled(game.deps.Skills, skill) {
			continue
		}
		allowed := true
		for _, req := range skill.Masteries {
			if req.Level > *c.Level || req.Level > 0 && req.ID != mastery {
				allowed = false
			}
		}
		if !allowed {
			continue
		}
		old := groups[skill.Group]
		if skill.Level > old.Level || old.ID == 0 {
			groups[skill.Group] = skill
		}
	}
	var skills []enterworld.SkillRow
	for _, skill := range groups {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		a, b := skills[i], skills[j]
		if a.Masteries[0].Level != b.Masteries[0].Level {
			return a.Masteries[0].Level > b.Masteries[0].Level
		}
		return a.ID < b.ID
	})
	if len(skills) == 0 {
		return fmt.Errorf("no supported caravan %s skills", family)
	}
	if game.caravanSkills == nil {
		game.caravanSkills = map[string][]uint32{}
	}
	for _, skill := range skills[:min(3, len(skills))] {
		c.Skills = append(c.Skills, skill.ID)
		game.caravanSkills[c.Name] = append(game.caravanSkills[c.Name], skill.ID)
	}
	return nil
}
