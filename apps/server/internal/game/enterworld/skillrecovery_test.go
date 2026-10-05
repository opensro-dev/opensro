/*
===========================================================================

skillrecovery_test.go - tests for skillrecovery.go

===========================================================================
*/

package enterworld

import (
	"strings"
	"testing"
)

/*
==================
TestRecoveryAdmissionRequiresCompleteUnmodifiedProgram
==================
*/
func TestRecoveryAdmissionRequiresCompleteUnmodifiedProgram(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "1"
	fields[8] = "2"
	fields[69] = "1751474540"
	fields[70] = "89"
	base := SkillRow{Consumption: SkillConsumption{Pinned: true}, TimingPinned: true, ActionCastingTimePinned: true, ActionDurationMs: 1000, ActionDurationPinned: true}
	row := base
	parseSkillRecovery(fields, &row)
	if !row.Recovery.SelfFlatPinned {
		t.Fatal("baseline refused")
	}
	for _, mutation := range []struct {
		column int
		value  string
	}{{8, "0"}, {15, "1"}, {22, "1"}, {33, "1"}, {56, "1"}, {71, "50"}, {70, "-1"}, {74, "1886743667"}, {117, "1"}} {
		changed := append([]string(nil), fields...)
		changed[mutation.column] = mutation.value
		row = base
		parseSkillRecovery(changed, &row)
		if row.Recovery.SelfFlatPinned {
			t.Fatalf("column %d was silently ignored", mutation.column)
		}
	}
	row = base
	row.ChainNext = 9
	parseSkillRecovery(fields, &row)
	if row.Recovery.SelfFlatPinned {
		t.Fatal("chain was reduced to self-heal")
	}
}

/*
==================
TestRecoveryProgramCannotDropAdditionalEffects
==================
*/
func TestRecoveryProgramCannotDropAdditionalEffects(t *testing.T) {
	source := sharedShippedSkills(t)
	count := 0
	for _, ref := range source.SpawnSkillRows() {
		row, _ := source.SkillByID(ref.ID)
		if row.Recovery.SelfFlatPinned {
			count++
		}
	}
	t.Logf("Complete flat self-recovery rows: %d", count)
	for _, name := range []string{"SKILL_CH_WATER_SELFHEAL_A_01", "SKILL_CH_WATER_SELFHEAL_D_01", "SKILL_EU_BARD_RECOVERA_ABNORMALTIME_A_01"} {
		row, ok := source.SkillByCodename(name)
		if !ok || !row.Recovery.SelfFlatPinned {
			t.Fatalf("flat self recovery missing: %s", name)
		}
	}
	for _, name := range []string{"SKILL_CH_WATER_HEAL_A_01", "SKILL_CH_WATER_RESURRECTION_A_01", "SKILL_EU_CLERIC_HEALA_CYCLE_A_01", "SKILL_EU_CLERIC_HEALA_GROUP_A_01"} {
		row, ok := source.SkillByCodename(name)
		if !ok || row.Recovery.SelfFlatPinned {
			t.Fatalf("compound recovery reduced to self heal: %s", name)
		}
	}
}

/*
==================
TestRaveMelodyAdmitsAFlatHPCost

Rave Melody is heal[0,0,594,0] paid with 495 HP (column 52). A flat HP
cost is charged by the action owner; a percent HP cost (column 54) still
refuses the row.
==================
*/
func TestRaveMelodyAdmitsAFlatHPCost(t *testing.T) {
	source := sharedShippedSkills(t)
	row, ok := source.SkillByCodename("SKILL_EU_BARD_RECOVERA_ABNORMALTIME_A_01")
	if !ok || !row.Recovery.SelfFlatPinned || row.Consumption.HP != 495 || row.Consumption.MP != 0 ||
		row.Heal.HP != 0 || row.Heal.MP != 594 || row.TargetRequired {
		t.Fatalf("rave melody %+v cost %+v heal %+v", row.Recovery, row.Consumption, row.Heal)
	}

	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8], fields[69], fields[72] = "1", "2", "1751474540", "594"
	base := SkillRow{Consumption: SkillConsumption{HP: 495, Pinned: true}, TimingPinned: true, ActionCastingTimePinned: true, ActionDurationMs: 2000, ActionDurationPinned: true}
	flat := base
	parseSkillRecovery(fields, &flat)
	if !flat.Recovery.SelfFlatPinned {
		t.Fatal("flat HP cost refused")
	}
	percent := base
	percent.Consumption.HPPercent = 10
	parseSkillRecovery(fields, &percent)
	if percent.Recovery.SelfFlatPinned {
		t.Fatal("percent HP cost admitted")
	}
}

/*
==================
TestPartyRecoveryRowsAreAdmittedByCompleteProgram

Over the shipped catalog, exactly the Group Healing / Group Healing Breath
/ Group Recovery / Holy Group Recovery lines and the Bard's Mana Breeze
(heal with an mwmh weapon term) compile to a party heal and
exactly the Group Reverse / Holy Group Reverse lines to a party
resurrection. Healing Orbit (efr dura puls heal, handler 3) is a heal
over time; Healing Division (eshp) and every targeted heal stay out.
==================
*/
func TestPartyRecoveryRowsAreAdmittedByCompleteProgram(t *testing.T) {
	source := sharedShippedSkills(t)
	healLines := []string{
		"SKILL_EU_CLERIC_HEALA_GROUP_A_",
		"SKILL_EU_CLERIC_HEALA_GROUP_B_",
		"SKILL_EU_CLERIC_RECOVERYA_QUICK_A_",
		"SKILL_EU_CLERIC_RECOVERYA_QUICK_B_",
		"SKILL_EU_BARD_RECOVERA_MANATRANS_B_",
	}
	resuLines := []string{
		"SKILL_EU_CLERIC_REBIRTHA_GROUP_A_",
		"SKILL_EU_CLERIC_REBIRTHA_GROUP_B_",
	}
	inLines := func(name string, lines []string) bool {
		for _, line := range lines {
			if strings.HasPrefix(name, line) {
				return true
			}
		}
		return false
	}

	heals, resus := 0, 0
	for _, ref := range source.SpawnSkillRows() {
		row, _ := source.SkillByID(ref.ID)
		if row.Recovery.PartyHealPinned != inLines(row.Codename, healLines) {
			t.Errorf("%s party heal %v", row.Codename, row.Recovery.PartyHealPinned)
		}
		if row.Recovery.PartyResurrectPinned != inLines(row.Codename, resuLines) {
			t.Errorf("%s party resurrect %v", row.Codename, row.Recovery.PartyResurrectPinned)
		}
		if row.Recovery.PartyHealPinned {
			heals++
		}
		if row.Recovery.PartyResurrectPinned {
			resus++
		}
	}
	// 8 + 4 + 8 + 1 + 4 heal tiers, 5 + 1 resurrection tiers.
	if heals != 25 || resus != 6 {
		t.Fatalf("party heals %d, party resurrections %d", heals, resus)
	}

	group, _ := source.SkillByCodename("SKILL_EU_CLERIC_HEALA_GROUP_A_01")
	area := group.Abnormal.EffectArea
	if !area.Present || area.Radius != 250 || area.Select != 5 || group.Heal.HP != 246 || !group.Heal.WeaponHP ||
		group.Abnormal.AdmitDeadParty || group.TargetRequired {
		t.Fatalf("group healing %+v %+v", area, group.Heal)
	}
	reverse, _ := source.SkillByCodename("SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01")
	area = reverse.Abnormal.EffectArea
	if !area.Present || area.Radius != 250 || area.Select != 4 || !reverse.Abnormal.AdmitDeadParty ||
		reverse.Abnormal.ResuMaxLevel != 60 || reverse.Heal.HP != 763 || reverse.Heal.MP != 763 {
		t.Fatalf("group reverse %+v %+v %+v", area, reverse.Abnormal, reverse.Heal)
	}
	for _, name := range []string{"SKILL_EU_CLERIC_HEALA_DIVIDE_A_01", "SKILL_EU_CLERIC_HEALA_TARGET_A_01"} {
		row, ok := source.SkillByCodename(name)
		if !ok || row.Recovery != (SkillRecovery{}) {
			t.Fatalf("%s admitted as recovery %+v", name, row.Recovery)
		}
	}
}

/*
==================
TestPartyRecoveryProgramRefusesUnknownInstructions

A synthetic Group Healing row: every extra or altered instruction, a timed
handler, a target column or a cap below the party bound refuses it.
==================
*/
func TestPartyRecoveryProgramRefusesUnknownInstructions(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8] = "1", "2"
	// efr[1,1,250,8,0,5] heal[246,0,0,0] mwhh[150] getv HLRU getv HLMD
	program := []string{"6645362", "1", "1", "250", "8", "0", "5", "1751474540", "246", "0", "0", "0", "1836542056", "150", "1734702198", "1212961365", "1734702198", "1212960068"}
	copy(fields[69:], program)
	base := SkillRow{Consumption: SkillConsumption{MP: 197, Pinned: true}, TimingPinned: true, ActionCastingTimeMs: 500, ActionCastingTimePinned: true, ActionDurationMs: 1166, ActionDurationPinned: true}
	row := base
	parseSkillRecovery(fields, &row)
	if row.Recovery != (SkillRecovery{PartyHealPinned: true}) {
		t.Fatalf("baseline %+v", row.Recovery)
	}

	for _, mutation := range []struct {
		column int
		value  string
	}{
		{68, "3"},          // a timed handler (Healing Orbit)
		{22, "1"},          // a target column
		{71, "2"},          // efr shape 2 (around the primary target)
		{72, "0"},          // no radius
		{73, "7"},          // a cap below the party bound
		{74, "50"},         // a secondary reduction
		{75, "24"},         // a hostile selection
		{85, "1702064240"}, // eshp in place of the second getv
		{86, "1212961365"}, // HLRU read twice
		{86, "1297432916"}, // getv MUAT, not consumed by a heal
		{87, "1886743667"}, // a trailing puls
	} {
		changed := append([]string(nil), fields...)
		changed[mutation.column] = mutation.value
		row = base
		parseSkillRecovery(changed, &row)
		if row.Recovery != (SkillRecovery{}) {
			t.Errorf("column %d = %s admitted %+v", mutation.column, mutation.value, row.Recovery)
		}
	}

	// efr[1,1,250,8,0,4] heal[763,0,763,0] resu[60,10]
	resu := append([]string(nil), fields...)
	for i := 69; i < 118; i++ {
		resu[i] = "0"
	}
	copy(resu[69:], []string{"6645362", "1", "1", "250", "8", "0", "4", "1751474540", "763", "0", "763", "0", "1919251317", "60", "10"})
	row = base
	parseSkillRecovery(resu, &row)
	if row.Recovery != (SkillRecovery{PartyResurrectPinned: true}) {
		t.Fatalf("resurrection %+v", row.Recovery)
	}
	resu[84] = "1919251572" // rmut after resu
	resu[85] = "10268"
	row = base
	parseSkillRecovery(resu, &row)
	if row.Recovery != (SkillRecovery{}) {
		t.Fatalf("resurrection with rmut admitted %+v", row.Recovery)
	}
}

/*
==================
TestHealOverTimeRowsAreAdmittedByCompleteProgram

Over the shipped catalog, exactly the Bard's Mana Cycle / Mana Orbit and
the Cleric's Healing Cycle / Healing Orbit lines compile to a heal over
time: targeted dura puls heal, or the party efr in front of it, with the
timed handler. Mana Cycle tier 1 pulses every 2000 ms for 16000 ms.
==================
*/
func TestHealOverTimeRowsAreAdmittedByCompleteProgram(t *testing.T) {
	source := sharedShippedSkills(t)
	lines := []string{
		"SKILL_EU_BARD_RECOVERA_MPHEAL_A_",
		"SKILL_EU_BARD_RECOVERA_MPHEAL_B_",
		"SKILL_EU_CLERIC_HEALA_CYCLE_A_",
		"SKILL_EU_CLERIC_HEALA_CYCLE_B_",
	}
	count := 0
	for _, ref := range source.SpawnSkillRows() {
		row, _ := source.SkillByID(ref.ID)
		in := false
		for _, line := range lines {
			in = in || strings.HasPrefix(row.Codename, line)
		}
		if row.Recovery.HealOverTimePinned != in {
			t.Errorf("%s heal over time %v", row.Codename, row.Recovery.HealOverTimePinned)
		}
		if row.Recovery.HealOverTimePinned {
			count++
		}
	}
	// 14 + 1 Bard tiers, 11 + 2 Cleric tiers.
	if count != 28 {
		t.Fatalf("heals over time %d", count)
	}

	cycle, _ := source.SkillByCodename("SKILL_EU_BARD_RECOVERA_MPHEAL_A_01")
	if cycle.Recovery != (SkillRecovery{HealOverTimePinned: true, PulseMs: 2000}) || cycle.EffectDurationMs != 16000 ||
		!cycle.TargetRequired || cycle.Heal.MP != 76 || !cycle.Heal.WeaponMP {
		t.Fatalf("mana cycle %+v %+v", cycle.Recovery, cycle.Heal)
	}
}

/*
==================
TestHealOverTimeProgramRefusesAlteredShapes

A synthetic Mana Orbit row: the instant handler, a missing or zero puls,
a puls longer than dura, a trailing instruction, a target column on the
party form and a targeted row leading with efr all refuse it.
==================
*/
func TestHealOverTimeProgramRefusesAlteredShapes(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8], fields[68] = "1", "2", "3"
	// efr[1,1,300,8,0,5] dura[16000] puls[2000] heal[0,0,2119,0] mwmh[90]
	program := []string{"6645362", "1", "1", "300", "8", "0", "5", "1685418593", "16000", "1886743667", "2000", "1751474540", "0", "0", "2119", "0", "1836543336", "90"}
	copy(fields[69:], program)
	base := SkillRow{Consumption: SkillConsumption{MP: 90, Pinned: true}, TimingPinned: true, ActionCastingTimePinned: true,
		ActionDurationMs: 1500, ActionDurationPinned: true, EffectDurationMs: 16000, EffectDurationPresent: true}
	row := base
	parseSkillRecovery(fields, &row)
	if row.Recovery != (SkillRecovery{HealOverTimePinned: true, PulseMs: 2000}) {
		t.Fatalf("baseline %+v", row.Recovery)
	}

	for _, mutation := range []struct {
		column int
		value  string
	}{
		{68, "0"},          // the instant handler
		{78, "0"},          // no puls tag
		{79, "0"},          // a zero period
		{79, "20000"},      // a period past the duration
		{77, "8000"},       // dura disagreeing with the effect duration
		{87, "1886743667"}, // a trailing puls
		{22, "1"},          // a target column on the party form
		{71, "2"},          // efr around the primary target
	} {
		changed := append([]string(nil), fields...)
		changed[mutation.column] = mutation.value
		row = base
		parseSkillRecovery(changed, &row)
		if row.Recovery.HealOverTimePinned {
			t.Errorf("column %d = %s admitted %+v", mutation.column, mutation.value, row.Recovery)
		}
	}

	// A targeted row must not carry the party efr.
	row = base
	row.TargetRequired = true
	parseSkillRecovery(fields, &row)
	if row.Recovery.HealOverTimePinned {
		t.Errorf("targeted row with efr admitted %+v", row.Recovery)
	}
}
