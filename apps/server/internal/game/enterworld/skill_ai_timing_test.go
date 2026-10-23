package enterworld

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"strings"
	"testing"
)

func TestAICommandDurationNativeVectors(t *testing.T) {
	var report struct {
		PortSHA256 string
		Cases      []struct {
			Kind                                  uint8
			Casting, Duration, Cooldown, Expected uint32
			Percent                               float32
		}
	}
	raw, err := licensed.ServerFixture(t, "internal/game/enterworld/testdata/native-ai-command-duration.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("skill_ai_timing.go")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(source)) != report.PortSHA256 {
		t.Fatal("AI duration candidate changed; rerun original-byte comparison", err)
	}
	if len(report.Cases) != 400 {
		t.Fatal("native duration corpus changed", len(report.Cases))
	}
	for _, c := range report.Cases {
		row := SkillRow{ActionKind: c.Kind, ActionCastingTimeMs: c.Casting, ActionDurationMs: c.Duration, CoolTimeMs: c.Cooldown}
		if got := row.AICommandDurationMs(c.Percent); got != c.Expected {
			t.Fatalf("%+v: got %d", c, got)
		}
		if got := compactSkill(row).value(); got.ActionKind != row.ActionKind {
			t.Fatal("storage lost raw action kind")
		}
	}
}

func TestAuthoredMonsterCommandTimerCoversPendingCast(t *testing.T) {
	skills := sharedShippedSkills(t)
	if err := skills.Load(); err != nil {
		t.Fatal(err)
	}
	checked, preparing := 0, 0
	for id, row := range skills.rows.values() {
		if !strings.HasPrefix(row.Codename, "MSKILL_") {
			continue
		}
		checked++
		if row.ActionKind != 2 {
			t.Fatalf("monster skill %d raw action kind=%d", id, row.ActionKind)
		}
		if row.ActionCastingTimeMs > 0 {
			preparing++
			if row.AICommandDurationMs(100) <= row.ActionCastingTimeMs {
				t.Fatalf("skill %d (%s) can issue another command before pending release: %+v", id, row.Codename, row)
			}
		}
	}
	if checked == 0 || preparing == 0 {
		t.Fatal("empty authored pending-cast audit")
	}
	t.Logf("%d authored monster skills; %d preparing casts covered by command timer", checked, preparing)
}
