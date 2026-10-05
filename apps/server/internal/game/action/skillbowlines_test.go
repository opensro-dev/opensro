/*
===========================================================================

skillbowlines_test.go - every Pacheon (bow) skill line the server can cast

Casts rank 1 of each SKILL_CH_BOW_* line from the shipped skilldata at a
monster in reach and runs five seconds of ticks: each attack must land and
spend its arrows, each buff must attach. Berserker Arrow (hr), Strong Bow C and
Arrow Combo D (ru on a bow), Arrow Combo C and D (chained bow shots) and
the attacking hawks (summ) were refused before.

===========================================================================
*/

package action

import (
	"sort"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

// bowLinesWithoutCast are the bow lines no cast admits: the passive.
var bowLinesWithoutCast = map[string]bool{
	"SKILL_CH_BOW_PASSIVE_A": true,
}

/*
================
TestEveryBowLineCasts
================
*/
func TestEveryBowLineCasts(t *testing.T) {
	all := shippedSkills(t)
	var roots []uint32
	for _, r := range all.SpawnSkillRows() {
		if row, ok := all.SkillByID(r.ID); ok && strings.HasPrefix(row.Codename, "SKILL_CH_BOW_") && strings.HasSuffix(row.Codename, "_01") && !row.ChainSub {
			roots = append(roots, r.ID)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	if len(roots) < 30 {
		t.Fatalf("only %d bow lines in the shipped skilldata", len(roots))
	}
	for _, id := range roots {
		row, _ := all.SkillByID(id)
		line := strings.TrimSuffix(row.Codename, "_01")
		t.Run(line, func(t *testing.T) {
			rt, c, target, _, _ := arrowFixture(t)
			skills := rt.deps.SkillData().(staticSkillSource)
			for _, r := range all.SpawnSkillRows() {
				if s, ok := all.SkillByID(r.ID); ok && strings.HasPrefix(s.Codename, line) {
					// The level-1 fixture's MP cannot pay later ranks' costs;
					// cost is not what this test is about.
					s.Consumption.MP, s.Consumption.MPPercent = 0, 0
					skills[s.ID] = s
				}
			}
			arrows := &c.MissionInventory[len(c.MissionInventory)-1]
			arrows.StackCount = 200
			c.Skills = append(c.Skills, id)
			cast := wire.SkillAction{ActionId: id}
			if row.TargetRequired {
				cast.HasTarget, cast.TargetGid = true, target
			}
			result := rt.HandleTargetInteract(testDivision, c, cast.Encode())
			opened := false
			for _, f := range result.Frames {
				opened = opened || f.Opcode == wire.OpSkillCastResult && len(f.Payload) > 0 && f.Payload[0] == 1
			}
			if bowLinesWithoutCast[line] {
				if opened {
					t.Fatalf("%s now casts: drop it from bowLinesWithoutCast", line)
				}
				return
			}
			if !opened {
				t.Fatalf("%s refused: %q %+v", row.Codename, result.DiagnosticRefusal, result.Frames)
			}
			now := rt.Now()
			for i := 0; i < 50; i++ {
				now = now.Add(100 * time.Millisecond)
				at := now
				rt.Now = func() time.Time { return at }
				rt.TickHook()(at.UnixMilli())
			}
			if !row.TargetRequired {
				if !hasSkillEffect(rt, c.Name, id) {
					t.Fatalf("%s attached no effect", row.Codename)
				}
				return
			}
			mob, _ := rt.Monsters.Get(testDivision, target)
			if mob.CurrentHP >= 100000 || arrows.StackCount >= 200 {
				t.Fatalf("%s: monster HP %d, arrows %d", row.Codename, mob.CurrentHP, arrows.StackCount)
			}
		})
	}
}

/*
================
TestBerserkerArrowRollsWithItsHitRate

593540 installs the engaged attack's hr through 594AC0: Berserker Arrow's hits
roll with the caster's hit rate raised by its flat word.
================
*/
func TestBerserkerArrowRollsWithItsHitRate(t *testing.T) {
	rt, c, _, _, _ := arrowFixture(t)
	skill := shippedOffense(t, "SKILL_CH_BOW_AREA_A_01")
	if !skill.BuffModifiers.Hr || skill.BuffModifiers.HrFlat == 0 || skill.OffenseRefusal != "" {
		t.Fatalf("Berserker Arrow row: %+v refusal %q", skill.BuffModifiers, skill.OffenseRefusal)
	}
	plain, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	engaged, _, err := rt.playerAttackStats(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	if engaged.HitRate < plain.HitRate+float64(skill.BuffModifiers.HrFlat) {
		t.Fatalf("hit rate %v engaged %v, want at least +%d", plain.HitRate, engaged.HitRate, skill.BuffModifiers.HrFlat)
	}
}
