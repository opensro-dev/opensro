package enterworld

import (
	"strconv"
	"testing"
)

func TestMinimapSkillTagsAndDurationBranches(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69] = strconv.Itoa(0x617474)
	fields[70] = strconv.Itoa(0x686e7470)
	if encodedTailContainsTag(fields, 0x686e7470) {
		t.Fatal("attack argument became hntp")
	}
	fields[75] = strconv.Itoa(0x686e7470)
	if !encodedTailContainsTag(fields, 0x686e7470) {
		t.Fatal("hntp tag lost")
	}
	fields[76] = strconv.Itoa(0x67657476)
	for _, c := range []struct {
		kind int
		want bool
	}{{0x52504255, true}, {0x53544455, true}, {0x44544452, false}} {
		fields[77] = strconv.Itoa(c.kind)
		if got := encodedStealthDuration(fields); got != c.want {
			t.Fatalf("kind %x duration=%v", c.kind, got)
		}
	}
}
func TestMinimapSkillMetadataPublication(t *testing.T) {
	skills := sharedShippedSkills(t)
	hunting := 0
	for _, row := range skills.SpawnSkillRows() {
		source, ok := skills.SkillByID(row.ID)
		if !ok || row.HuntingPoint != source.HuntingPoint || row.StealthDuration != source.StealthDuration {
			t.Fatalf("minimap metadata lost for %d", row.ID)
		}
		if row.HuntingPoint {
			hunting++
		}
	}
	if hunting == 0 {
		t.Fatal("retail hunting skill metadata absent")
	}
}

/*
================
TestSkillUiRowCarriesMPCost

The client stands no cooldown in for a press its caster cannot pay for, so
every player skill's UI row carries its authored MP cost (Crystal Wall, 99,
costs MP). Monster rows carry none.
================
*/
func TestSkillUiRowCarriesMPCost(t *testing.T) {
	skills := sharedShippedSkills(t)
	for _, row := range skills.SpawnSkillRows() {
		source, ok := skills.SkillByID(row.ID)
		if !ok || row.UI == nil {
			continue
		}
		if !source.Consumption.Pinned || !playerSkillCodename(source.Codename) {
			if row.UI.MP != 0 || row.UI.MPPercent != 0 {
				t.Fatalf("skill %d (%s) published an MP cost", row.ID, source.Codename)
			}
			continue
		}
		if row.UI.MP != source.Consumption.MP || row.UI.MPPercent != source.Consumption.MPPercent {
			t.Fatalf("skill %d MP cost %d/%d published as %d/%d", row.ID, source.Consumption.MP,
				source.Consumption.MPPercent, row.UI.MP, row.UI.MPPercent)
		}
		if row.ID == 99 && row.UI.MP == 0 {
			t.Fatal("Crystal Wall published no MP cost")
		}
	}
}

/*
================
TestSkillUiRowCarriesTargetGroups

Cold Wave Arrest admits a living monster or an enemy player, never its
caster: the client predicts its cast only at such a target.
================
*/
func TestSkillUiRowCarriesTargetGroups(t *testing.T) {
	skills := sharedShippedSkills(t)
	row, ok := skills.SkillByCodename("SKILL_CH_COLD_GIGONGJANG_A_01")
	if !ok {
		t.Fatal("Cold Wave Arrest missing")
	}
	for _, spawn := range skills.SpawnSkillRows() {
		if spawn.ID != row.ID {
			continue
		}
		want := uint16(SkillUiTargetAnimal | SkillUiTargetMonster | SkillUiTargetPlayer)
		if spawn.UI == nil || spawn.UI.Targets != want {
			t.Fatalf("Cold Wave Arrest targets %+v, want %b", spawn.UI, want)
		}
		return
	}
	t.Fatal("Cold Wave Arrest has no catalogue row")
}
