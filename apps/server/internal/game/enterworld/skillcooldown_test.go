/*
===========================================================================

skillcooldown_test.go - world entry restores surviving server reuse deadlines

Test the real bootstrap and its JSON, including shared-group precedence,
learned upgrades, exact expiry and preservation of the stored deadlines.

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

/*
================
TestBootstrapRestoresSkillCooldownsWithoutRestartingThem
================
*/
func TestBootstrapRestoresSkillCooldownsWithoutRestartingThem(t *testing.T) {
	character := chinaSpearman()
	character.Skills = []uint32{5, 4, 3, 2, 1, 99}
	character.OffensiveSkillCooldowns = map[uint32]int64{1: 181000, 2: 999999, 3: 999999, 4: 1000, 5: 999999}
	character.SharedSkillCooldowns = map[uint8]int64{7: 61000}
	before := character.Snapshot()
	deps := testDeps(character)
	deps.Now = func() time.Time { return time.UnixMilli(1000) }
	deps.Skills = fakeSeedSkills{
		"flying":  {ID: 1, Codename: "SKILL_CH_TEST_FLYING", Group: 1, CoolTimeMs: 180000},
		"shared":  {ID: 2, Codename: "SKILL_CH_TEST_SHARED", Group: 2, CoolTimeGroup: 7, CoolTimeMs: 90000},
		"absent":  {ID: 3, Codename: "SKILL_CH_TEST_ABSENT", Group: 3, CoolTimeGroup: 8, CoolTimeMs: 90000},
		"expired": {ID: 4, Codename: "SKILL_CH_TEST_EXPIRED", Group: 4, CoolTimeMs: 90000},
		"zero":    {ID: 5, Codename: "SKILL_CH_TEST_ZERO", Group: 5, CoolTimeMs: 0},
	}
	result := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if result.NativeResult != nativeResultSuccess {
		t.Fatalf("bootstrap refused: %+v", result)
	}
	want := []EntrySkillCooldown{
		{Skill: 1, RemainingMs: 180000, DurationMs: 180000},
		{Skill: 2, RemainingMs: 60000, DurationMs: 90000},
	}
	if !reflect.DeepEqual(result.SkillCooldowns, want) {
		t.Fatalf("entry cooldowns %+v, want %+v", result.SkillCooldowns, want)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ProtocolVersion int                  `json:"protocolVersion"`
		SkillCooldowns  []EntrySkillCooldown `json:"skillCooldowns"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ProtocolVersion != BootstrapProtocolVersion || !reflect.DeepEqual(decoded.SkillCooldowns, want) {
		t.Fatalf("JSON cooldown contract %+v", decoded)
	}
	if !reflect.DeepEqual(character.OffensiveSkillCooldowns, before.OffensiveSkillCooldowns) ||
		!reflect.DeepEqual(character.SharedSkillCooldowns, before.SharedSkillCooldowns) {
		t.Fatal("bootstrap changed authority deadlines")
	}
	deps.Now = func() time.Time { return time.UnixMilli(181000) }
	result = Build(deps, BootstrapRequest{CharacterName: character.Name})
	if result.NativeResult != nativeResultSuccess || len(result.SkillCooldowns) != 0 {
		t.Fatalf("exact deadline did not expire: %+v", result)
	}
}

/*
================
TestBootstrapCooldownGroupSurvivesLearnedUpgrade
================
*/
func TestBootstrapCooldownGroupSurvivesLearnedUpgrade(t *testing.T) {
	character := chinaSpearman()
	character.Skills = []uint32{2}
	character.OffensiveSkillCooldowns = map[uint32]int64{10: 181000}
	deps := testDeps(character)
	deps.Now = func() time.Time { return time.UnixMilli(61000) }
	deps.Skills = fakeSeedSkills{"upgrade": {ID: 2, Codename: "SKILL_CH_TEST_UPGRADE", Group: 10, CoolTimeMs: 180000}}
	result := Build(deps, BootstrapRequest{CharacterName: character.Name})
	want := []EntrySkillCooldown{{Skill: 2, RemainingMs: 120000, DurationMs: 180000}}
	if result.NativeResult != nativeResultSuccess || !reflect.DeepEqual(result.SkillCooldowns, want) {
		t.Fatalf("learned upgrade lost reuse deadline: %+v", result)
	}
}

/*
================
TestBootstrapCooldownsOnlyProjectClientCatalogueSkills

A granted skill without UI still owns server reuse, but sending its timer
would make the client's strict catalogue lookup refuse the entire entry.
================
*/
func TestBootstrapCooldownsOnlyProjectClientCatalogueSkills(t *testing.T) {
	skills := &TextdataSkills{}
	skills.once.Do(func() {})
	for _, row := range []SkillRow{
		{ID: 1, Codename: "SKILL_MONSTER_HIDDEN", Group: 10, CoolTimeGroup: 7, CoolTimeMs: 60000},
		{ID: 2, Codename: "SKILL_CH_RESTORE_TEST", Group: 20, CoolTimeGroup: 7, CoolTimeMs: 60000},
		{ID: 3, Codename: "SKILL_EU_RESTORE_TEST", Group: 30, CoolTimeMs: 60000},
		{ID: 4, Codename: "SKILL_MONSTER_ICON", Icon: "icon.ddj", Group: 40, CoolTimeMs: 60000},
	} {
		skills.rows.set(row.ID, row)
	}
	character := chinaSpearman()
	character.Skills = []uint32{1, 2, 3, 4}
	character.OffensiveSkillCooldowns = map[uint32]int64{30: 61000, 40: 61000}
	character.SharedSkillCooldowns = map[uint8]int64{7: 61000}
	before := character.Snapshot()
	deps := testDeps(character)
	deps.Skills = skills
	deps.Now = func() time.Time { return time.UnixMilli(1000) }
	result := Build(deps, BootstrapRequest{CharacterName: character.Name})
	want := []EntrySkillCooldown{
		{Skill: 2, RemainingMs: 60000, DurationMs: 60000},
		{Skill: 3, RemainingMs: 60000, DurationMs: 60000},
		{Skill: 4, RemainingMs: 60000, DurationMs: 60000},
	}
	if result.NativeResult != nativeResultSuccess || !reflect.DeepEqual(result.SkillCooldowns, want) {
		t.Fatalf("non-catalogue cooldown reached entry: %+v", result.SkillCooldowns)
	}
	ui := map[uint32]bool{}
	for _, row := range result.RefSkillSnapshot {
		ui[row.ID] = row.UI != nil
	}
	for _, row := range result.SkillCooldowns {
		if !ui[row.Skill] {
			t.Fatalf("cooldown skill %d has no client metadata", row.Skill)
		}
	}
	if !reflect.DeepEqual(character.OffensiveSkillCooldowns, before.OffensiveSkillCooldowns) ||
		!reflect.DeepEqual(character.SharedSkillCooldowns, before.SharedSkillCooldowns) {
		t.Fatal("projection discarded a hidden skill's authoritative deadline")
	}
}
