/*
===========================================================================

skillmastery_test.go - the loaded table's highest mastery requirement

The startup pairing warning reads MaxMasteryRequirement; both mastery
slots count, and an absent table reads as zero rather than failing.

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

/*
================
masteryRowFields

One authored 118-column row whose two mastery slots carry the given
(id, level) pairs; a zero id leaves the slot empty.
================
*/
func masteryRowFields(id uint32, codename string, first, second [2]int64) string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "1"
	fields[1] = strconv.Itoa(int(id))
	fields[3] = codename
	fields[34] = strconv.Itoa(int(first[0]))
	fields[36] = strconv.Itoa(int(first[1]))
	fields[35] = strconv.Itoa(int(second[0]))
	fields[37] = strconv.Itoa(int(second[1]))
	return strings.Join(fields, "\t")
}

/*
================
loadMasteryRows
================
*/
func loadMasteryRows(t *testing.T, rows ...string) *TextdataSkills {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skilldata.txt"), []byte("shard_a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shard_a.txt"), []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := NewTextdataSkills(dir)
	if err := skills.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return skills
}

/*
================
TestMaxMasteryRequirementReadsBothSlots
================
*/
func TestMaxMasteryRequirementReadsBothSlots(t *testing.T) {
	skills := loadMasteryRows(t,
		masteryRowFields(9101, "SYN_MASTERY_A", [2]int64{518, 90}, [2]int64{}),
		masteryRowFields(9102, "SYN_MASTERY_B", [2]int64{518, 110}, [2]int64{}),
		masteryRowFields(9103, "SYN_MASTERY_C", [2]int64{}, [2]int64{514, 105}),
	)
	if got := skills.MaxMasteryRequirement(); got != 110 {
		t.Fatalf("highest requirement %d, want 110", got)
	}

	second := loadMasteryRows(t,
		masteryRowFields(9201, "SYN_MASTERY_D", [2]int64{518, 90}, [2]int64{}),
		masteryRowFields(9202, "SYN_MASTERY_E", [2]int64{}, [2]int64{514, 105}),
	)
	if got := second.MaxMasteryRequirement(); got != 105 {
		t.Fatalf("highest requirement %d, want the second slot's 105", got)
	}

	missing := NewTextdataSkills(filepath.Join(t.TempDir(), "absent"))
	if got := missing.MaxMasteryRequirement(); got != 0 {
		t.Fatalf("absent table requirement %d, want 0", got)
	}
}
