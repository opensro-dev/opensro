/*
===========================================================================

skill_preparation_test.go - native post-load preparation/casting normalization

All execution handlers consume the normalized record, including rows whose
raw casting field is zero. Keep preparation out of recovery and cooldown.

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

/*
================
TestSkillPreparationNormalization
================
*/
func TestSkillPreparationNormalization(t *testing.T) {
	for _, tc := range []struct {
		prepare, casting string
		want             uint32
		pinned           bool
	}{
		{"670", "300", 970, true},
		{"500", "0", 500, true},
		{"0", "300", 300, true},
		{"0", "0", 0, true},
		{"4294967295", "2", 1, true},
		{"-1", "300", 0, false},
		{"bad", "300", 0, false},
		{"4294967296", "300", 0, false},
	} {
		t.Run(tc.prepare+"/"+tc.casting, func(t *testing.T) {
			dir := t.TempDir()
			row := syntheticSkillRow(map[int]string{1: "999", 3: "NORMALIZED", 11: tc.prepare, 12: tc.casting, 13: "530", 14: "4000"})
			if err := os.WriteFile(filepath.Join(dir, "skilldata_virtual.txt"), []byte(row+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			skills := NewTextdataSkills(dir)
			for i := 0; i < 2; i++ {
				if err := skills.Load(); err != nil {
					t.Fatal(err)
				}
				got, ok := skills.SkillByID(999)
				if !ok || got.ActionCastingTimeMs != tc.want || got.ActionCastingTimePinned != tc.pinned || got.ActionDurationMs != 530 || got.CoolTimeMs != 4000 {
					t.Fatalf("normalization pass %d: %+v", i, got)
				}
			}
		})
	}
}

/*
================
TestEveryShippedSkillIncludesPreparationExactlyOnce
================
*/
func TestEveryShippedSkillIncludesPreparationExactlyOnce(t *testing.T) {
	skills := sharedShippedSkills(t)
	seen := map[uint32]bool{}
	changed := 0
	for _, shard := range skillShards(skills.dir) {
		for _, f := range readTextdataFile(filepath.Join(skills.dir, shard)) {
			if len(f) < 47 {
				continue
			}
			id := textdataU32(f[1])
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			prepare, e1 := strconv.ParseUint(f[11], 10, 32)
			casting, e2 := strconv.ParseUint(f[12], 10, 32)
			if e1 != nil || e2 != nil {
				t.Fatalf("invalid shipped timing %d", id)
			}
			row, ok := skills.SkillByID(id)
			if !ok || !row.ActionCastingTimePinned || row.ActionCastingTimeMs != uint32(prepare+casting) {
				t.Fatalf("%d preparation lost: %d + %d -> %d", id, prepare, casting, row.ActionCastingTimeMs)
			}
			if prepare != 0 {
				changed++
			}
		}
	}
	if len(seen) != 27835 || changed == 0 {
		t.Fatalf("unexpected census: %d rows, %d changed", len(seen), changed)
	}
	t.Logf("all %d skills normalized; %d contain nonzero preparation", len(seen), changed)
}
