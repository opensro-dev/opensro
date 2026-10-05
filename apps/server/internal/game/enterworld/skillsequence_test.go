package enterworld

import (
	"fmt"
	"testing"
)

type sequenceSource map[uint32]SkillRow

func (s sequenceSource) SkillByID(id uint32) (SkillRow, bool) { r, ok := s[id]; return r, ok }

func TestOffensiveSequenceRejectsIncompleteGraphs(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "cycle", "foreign-group", "foreign-level", "unsupported-tail", "paid-tail", "hp-ratio-tail", "repeated-hp-ratio", "foreign-hp-ratio", "unmarked-tail", "zero-duration", "sub-root"} {
		t.Run(mode, func(t *testing.T) {
			root := SkillRow{ID: 6, Group: 177, Level: 1, ChainNext: 7, OffensiveStagePinned: true,
				Consumption: SkillConsumption{Pinned: true, MP: 32}, ActionCastingTimePinned: true, ActionDurationPinned: true, ActionDurationMs: 428}
			tail := root
			tail.ID = 7
			tail.ChainNext = 0
			tail.ChainSub = true
			tail.Consumption.MP = 0
			switch mode {
			case "missing":
				root.ChainNext = 99
			case "cycle":
				tail.ChainNext = 6
			case "foreign-group":
				tail.Group++
			case "foreign-level":
				tail.Level++
			case "unsupported-tail":
				tail.OffensiveStagePinned = false
			case "paid-tail":
				tail.Consumption.MP = 1
			case "hp-ratio-tail":
				tail.Consumption.HPPercent = 10
			case "repeated-hp-ratio":
				// Dare Devil: the root's HP ratio, repeated, is charged once.
				root.Consumption.HPPercent, tail.Consumption.HPPercent = 10, 10
			case "foreign-hp-ratio":
				root.Consumption.HPPercent, tail.Consumption.HPPercent = 10, 20
			case "unmarked-tail":
				tail.ChainSub = false
			case "zero-duration":
				tail.ActionDurationMs = 0
			case "sub-root":
				root.ChainSub = true
			}
			rows, ok := OffensiveSequence(sequenceSource{6: root, 7: tail}, 6)
			if ok != (mode == "valid" || mode == "repeated-hp-ratio") {
				t.Fatalf("%s accepted=%v rows=%v", mode, ok, rows)
			}
		})
	}
}

/*
================
TestShippedHPRatioChainsAreOffensive

Dare Devil and Crutial Rush tiers: the root and its second stage form a
two-stage offensive plan; only the root carries the MP price, and the
second stage repeats the root's 10 percent HP ratio.
================
*/
func TestShippedHPRatioChainsAreOffensive(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, line := range []string{"SKILL_EU_WARRIOR_TWOHANDA_CRY_B", "SKILL_EU_WARRIOR_DUALA_WHIRLWIND_B"} {
		for tier := 1; tier <= 3; tier++ {
			code := fmt.Sprintf("%s_%02d", line, tier)
			root, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			stages, ok := OffensiveSequence(source, root.ID)
			if !ok || len(stages) != 2 || source.ExecutionPlan(root.ID).Kind() != SkillExecutionOffense {
				t.Fatalf("%s: offensive=%v stages=%d", code, ok, len(stages))
			}
			tail := stages[1].Consumption
			if root.Consumption.HPPercent != 10 || root.Consumption.MP == 0 || tail.HPPercent != 10 || tail.MP != 0 {
				t.Fatalf("%s: root %+v tail %+v", code, root.Consumption, tail)
			}
		}
	}
}
