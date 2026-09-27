/*
===========================================================================

skilldata_paths_test.go - portable lookup of authored skill index entries.

The immutable projection uses lowercase names even when the retail index
uses Windows casing. The complete loader must work on a case-sensitive host.

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestSkillIndexLoadsCanonicalProjectionFilenames
================
*/
func TestSkillIndexLoadsCanonicalProjectionFilenames(t *testing.T) {
	directory := t.TempDir()
	row := syntheticSkillRow(map[int]string{
		1: "4", 2: "175", 3: "SYN_CASE_01", 7: "1", 9: "0",
		12: "175", 13: "1200", 14: "1350", 18: "33554432",
		34: "0", 35: "0", 36: "0", 37: "0", 38: "0", 39: "0",
		40: "0", 41: "0", 42: "0", 43: "0", 44: "0", 45: "0", 46: "117",
	})
	for filename, content := range map[string]string{
		"skilldata.txt":      "SkillData_5000.txt\n",
		"skilldata_5000.txt": row + "\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	table := NewTextdataSkills(directory)
	if err := table.Load(); err != nil {
		t.Fatal(err)
	}
	loaded, ok := table.SkillByID(4)
	if !ok || loaded.Codename != "SYN_CASE_01" || loaded.SPCost != 117 {
		t.Fatalf("indexed skill = %+v, found = %v", loaded, ok)
	}
}
