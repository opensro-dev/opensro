/*
===========================================================================

skill_ui_test.go - the skill rows' UI projection the client reads

The projection (SkillUiRow) must carry the training, targeting and buff
cancellation authority of the same table the server admits casts with.

===========================================================================
*/

package enterworld

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
================
TestSkillReferenceOptionalFlagsPreserveWireMeaning

These two optional flags default to false in attachedEffectReferences.
Omitting false values leaves room for the required linked-cast metadata
without raising the full-table reference budget or dropping skill rows.
================
*/
func TestSkillReferenceOptionalFlagsPreserveWireMeaning(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		row := SpawnSkillRow{ID: 12, LinkedSkillID: 6, HuntingPoint: enabled, StealthDuration: enabled}
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err = json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"huntingPoint", "stealthDuration"} {
			v, present := object[key]
			if present != enabled || (enabled && string(v) != "true") {
				t.Fatalf("%s lost optional flag semantics: %s", key, data)
			}
		}
		for _, key := range []string{"token", "status", "effectRider"} {
			if string(object[key]) != "false" {
				t.Fatalf("required discriminator %s was omitted: %s", key, data)
			}
		}
		if string(object["linkedSkillId"]) != "6" {
			t.Fatalf("linked root was dropped: %s", data)
		}
		var decoded SpawnSkillRow
		if err = json.Unmarshal(data, &decoded); err != nil || decoded != row {
			t.Fatalf("round trip changed authority: %+v err=%v", decoded, err)
		}
	}
}

/*
================
TestSkillUiProjectionUsesTrainingAuthority
================
*/
func TestSkillUiProjectionUsesTrainingAuthority(t *testing.T) {
	source := sharedShippedSkills(t)
	rows := source.SpawnSkillRows()
	count := 0
	for _, row := range rows {
		if row.UI == nil {
			continue
		}
		count++
		authority, ok := source.SkillByID(row.ID)
		if !ok || row.UI.SPCost != authority.SPCost || row.UI.Masteries != authority.Masteries || row.UI.Prerequisites != authority.Prerequisites || row.UI.Trainable != (!authority.ChainSub && authority.SPCost > 0) || row.UI.TargetRequired != authority.TargetRequired || row.UI.TargetSelf != (authority.TargetRequired && authority.Targets.Self) || row.UI.CooldownMs != authority.CoolTimeMs {
			t.Fatalf("divergent skill UI %d", row.ID)
		}
	}
	if count == 0 {
		t.Fatal("no player metadata")
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d skills, %d UI rows, %d bytes", len(rows), count, len(encoded))
	// 12.6 MB once the player skills' MP costs (29 KB) and target groups joined.
	if len(encoded) > 13<<20 {
		t.Fatal("skill reference budget exceeded")
	}
	var wire []struct {
		ID uint32 `json:"id"`
		UI *struct {
			Masteries     []SkillRequirement `json:"masteries"`
			Prerequisites []SkillRequirement `json:"prerequisites"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, row := range wire {
		if row.UI != nil && (len(row.UI.Masteries) != 2 || len(row.UI.Prerequisites) != 3) {
			t.Fatalf("skill %d dropped requirement slots during serialization", row.ID)
		}
	}
	for i := range rows {
		rows[i].UI = nil
	}
	bare, _ := json.Marshal(rows)
	t.Logf("spawn-only references: %d bytes", len(bare))
}

/*
================
TestBuffCancellationProjectionUsesNativeMarkers
================
*/
func TestBuffCancellationProjectionUsesNativeMarkers(t *testing.T) {
	source := sharedShippedSkills(t)
	counts := map[string]int{}
	for _, ref := range source.SpawnSkillRows() {
		if ref.UI == nil {
			continue
		}
		row, _ := source.SkillByID(ref.ID)
		want := ""
		if row.VoluntaryCancelBlocked && !row.BuffCancelInstance {
			want = "blocked"
		} else if row.BuffCancelConfirm {
			want = "confirm"
		}
		if ref.UI.BuffCancel != want || ref.UI.BuffCancelInstance != row.BuffCancelInstance {
			t.Fatalf("cancel projection %d: %+v", ref.ID, ref.UI)
		}
		counts[want]++
	}
	for _, mode := range []string{"", "blocked", "confirm"} {
		if counts[mode] == 0 {
			t.Fatalf("no authored %q cancellation branch", mode)
		}
	}
	// Payload values equal to tags must not manufacture cancellation markers.
	fields := make([]string, 76)
	fields[69] = "1685418593"
	fields[70] = "1667396966"
	fields[71] = "0"
	if encodedTailContainsTag(fields, 0x63627566) {
		t.Fatal("duration argument treated as cbuf")
	}
}

/*
================
TestSkillUiProjectionMarksSelfTargets

Mana Cycle and Discord Wave require a target that may be their caster; the
client reads targetSelf to aim them at itself when nothing is selected. An
enemy-only targeted row (Smashing Series) carries no such mark.
================
*/
func TestSkillUiProjectionMarksSelfTargets(t *testing.T) {
	source := sharedShippedSkills(t)
	want := map[string]bool{
		"SKILL_EU_BARD_RECOVERA_MPHEAL_A_01": true,
		"SKILL_EU_BARD_FORGETA_AGGRO_A_11":   true,
		"SKILL_CH_SWORD_SMASH_A_01":          false,
	}
	seen := map[string]bool{}
	for _, ref := range source.SpawnSkillRows() {
		row, ok := source.SkillByID(ref.ID)
		self, listed := want[row.Codename]
		if !ok || !listed || ref.UI == nil {
			continue
		}
		seen[row.Codename] = true
		if !ref.UI.TargetRequired || ref.UI.TargetSelf != self {
			t.Errorf("%s: targetRequired %v targetSelf %v, want true and %v", row.Codename, ref.UI.TargetRequired, ref.UI.TargetSelf, self)
		}
	}
	for code := range want {
		if !seen[code] {
			t.Errorf("%s has no UI row", code)
		}
	}
}

/*
================
TestSkillUiRangeBonusFollowsTheRowsParameters

The server adds the caster's crossbow-range and wizard-range parameters to
a cast's reach when the row asks for them, so the UI projection must mark
exactly the asking rows (rangeBonus) and keep the mark off the wire for
every other row, whose published JSON does not change.
================
*/
func TestSkillUiRangeBonusFollowsTheRowsParameters(t *testing.T) {
	source := sharedShippedSkills(t)
	marked := 0
	for _, ref := range source.SpawnSkillRows() {
		row, ok := source.SkillByID(ref.ID)
		if !ok || ref.UI == nil {
			continue
		}
		asks := row.Attack.Parameters.Has(ParameterCrossbowRange) || row.Attack.Parameters.Has(ParameterWizardRange)
		if ref.UI.RangeBonus != asks {
			t.Errorf("%s: rangeBonus %v, want %v", row.Codename, ref.UI.RangeBonus, asks)
		}
		if !asks {
			continue
		}
		marked++
		data, err := json.Marshal(ref.UI)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"rangeBonus":true`) {
			t.Errorf("%s: UI JSON lost the mark: %s", row.Codename, data)
		}
	}
	if marked == 0 {
		t.Fatal("no shipped row carries the rangeBonus mark")
	}
	for _, ref := range source.SpawnSkillRows() {
		if ref.UI == nil || ref.UI.RangeBonus {
			continue
		}
		data, err := json.Marshal(ref.UI)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "rangeBonus") {
			row, _ := source.SkillByID(ref.ID)
			t.Errorf("%s: UI JSON carries an unmarked row's field: %s", row.Codename, data)
		}
	}
}

/*
================
TestSkillUiChaseMarksFollowTheRows

The predicted attack chase claims its reach from the resolved basic row
and refuses while a range-up effect stands on the caster, so the UI
projection must mark exactly the pinned base attacks (basicAttack) and
the rows whose buff feeds the attack-range keeper (rangeUp), and keep
both marks off the wire for every other row.
================
*/
func TestSkillUiChaseMarksFollowTheRows(t *testing.T) {
	source := sharedShippedSkills(t)
	basicAttacks := map[string]bool{}
	for _, raceKey := range []string{RaceKeyChina, RaceKeyEurope} {
		for _, codename := range DefaultSkillCodenames(raceKey) {
			basicAttacks[codename] = true
		}
	}
	basicMarked := 0
	for _, ref := range source.SpawnSkillRows() {
		row, ok := source.SkillByID(ref.ID)
		if !ok || ref.UI == nil {
			continue
		}
		if ref.UI.BasicAttack != basicAttacks[row.Codename] {
			t.Errorf("%s: basicAttack %v, want %v", row.Codename, ref.UI.BasicAttack, basicAttacks[row.Codename])
		}
		if ref.UI.RangeUp != row.BuffModifiers.Ru {
			t.Errorf("%s: rangeUp %v, want %v", row.Codename, ref.UI.RangeUp, row.BuffModifiers.Ru)
		}
		data, err := json.Marshal(ref.UI)
		if err != nil {
			t.Fatal(err)
		}
		for field, marked := range map[string]bool{"basicAttack": ref.UI.BasicAttack, "rangeUp": ref.UI.RangeUp} {
			if marked && !strings.Contains(string(data), `"`+field+`":true`) {
				t.Errorf("%s: UI JSON lost the %s mark: %s", row.Codename, field, data)
			}
			if !marked && strings.Contains(string(data), field) {
				t.Errorf("%s: UI JSON carries an unmarked row's %s field: %s", row.Codename, field, data)
			}
		}
		if ref.UI.BasicAttack {
			basicMarked++
		}
	}
	if basicMarked == 0 {
		t.Fatal("no shipped row carries the basicAttack mark")
	}
}
