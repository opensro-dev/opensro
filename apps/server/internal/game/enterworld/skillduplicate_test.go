/*
===========================================================================

skillduplicate_test.go - the Duplicate target envelope

The native ranks copy an allied player; the later rank copies an enemy
player. Both envelopes pin through the production loader; a
monster-targeted or duration-less program never does.

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
duplicateProgramFields

One authored 118-column Duplicate row: the shipped program (msch 2
{level}, dura, skc, reqi) over the given target columns. Cells the
loader reads carry the shipped row's values or zero, and the program
tail stays zero past the last word.
================
*/
func duplicateProgramFields(id uint32, codename string, targets map[int]string) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "1"
	fields[1] = strconv.Itoa(int(id))
	fields[2] = "66"
	fields[3] = codename
	fields[7] = "1"
	fields[13] = "500"
	fields[46] = "10"
	fields[53] = "100"
	fields[67] = "66"
	fields[68] = "3"
	for col, value := range targets {
		fields[col] = value
	}
	program := []uint32{0x6d736368, 2, 60, tagDura, 900000, tagSkc, 0, 2, 0, tagReqi, 6, 12, tagReqi, 6, 13}
	for i, word := range program {
		fields[69+i] = strconv.FormatUint(uint64(word), 10)
	}
	return fields
}

/*
================
joinSkillRow
================
*/
func joinSkillRow(fields []string) string {
	return strings.Join(fields, "\t")
}

/*
================
loadDuplicateRows
================
*/
func loadDuplicateRows(t *testing.T, rows ...string) *TextdataSkills {
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
TestDuplicateParserAdmitsTheEnemyPlayerEnvelope

The allied envelope (ally or party) and the enemy-player envelope
both pin with the program's level word; a monster-targeted program
is outside this family and stays unpinned.
================
*/
func TestDuplicateParserAdmitsTheEnemyPlayerEnvelope(t *testing.T) {
	skills := loadDuplicateRows(t,
		joinSkillRow(duplicateProgramFields(9101, "SYN_DUPLE_ALLY", map[int]string{22: "1", 23: "1", 27: "1", 28: "1"})),
		joinSkillRow(duplicateProgramFields(9102, "SYN_DUPLE_ENEMY", map[int]string{22: "1", 23: "1", 30: "1"})),
		joinSkillRow(duplicateProgramFields(9103, "SYN_DUPLE_MONSTER", map[int]string{22: "1", 23: "1", 29: "1"})),
	)
	for _, tc := range []struct {
		id     uint32
		pinned bool
	}{
		{9101, true},
		{9102, true},
		{9103, false},
	} {
		row, ok := skills.SkillByID(tc.id)
		if !ok {
			t.Fatalf("row %d did not load", tc.id)
		}
		if row.Duplicate.Pinned != tc.pinned {
			t.Errorf("row %d (%s) pinned %v, want %v", tc.id, row.Codename, row.Duplicate.Pinned, tc.pinned)
		}
		if tc.pinned && row.Duplicate.MaxLevel != 60 {
			t.Errorf("row %d max level %d, want the program's 60", tc.id, row.Duplicate.MaxLevel)
		}
	}
}

/*
================
TestDuplicateParserStillRequiresTheDuration

An enemy-player row without the dura word is a partial program: the
instance would have no lifetime, so it stays unpinned.
================
*/
func TestDuplicateParserStillRequiresTheDuration(t *testing.T) {
	fields := duplicateProgramFields(9104, "SYN_DUPLE_NODURA", map[int]string{22: "1", 23: "1", 30: "1"})
	fields[72], fields[73] = "0", "0" // the dura tag and its word
	skills := loadDuplicateRows(t, joinSkillRow(fields))
	row, ok := skills.SkillByID(9104)
	if !ok {
		t.Fatal("row 9104 did not load")
	}
	if row.Duplicate.Pinned {
		t.Fatalf("duration-less program pinned: %+v", row.Duplicate)
	}
}
