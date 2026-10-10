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
		"flying":  {ID: 1, Group: 1, CoolTimeMs: 180000},
		"shared":  {ID: 2, Group: 2, CoolTimeGroup: 7, CoolTimeMs: 90000},
		"absent":  {ID: 3, Group: 3, CoolTimeGroup: 8, CoolTimeMs: 90000},
		"expired": {ID: 4, Group: 4, CoolTimeMs: 90000},
		"zero":    {ID: 5, Group: 5, CoolTimeMs: 0},
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
	deps.Skills = fakeSeedSkills{"upgrade": {ID: 2, Group: 10, CoolTimeMs: 180000}}
	result := Build(deps, BootstrapRequest{CharacterName: character.Name})
	want := []EntrySkillCooldown{{Skill: 2, RemainingMs: 120000, DurationMs: 180000}}
	if result.NativeResult != nativeResultSuccess || !reflect.DeepEqual(result.SkillCooldowns, want) {
		t.Fatalf("learned upgrade lost reuse deadline: %+v", result)
	}
}
