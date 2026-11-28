/*
===========================================================================

characterids_test.go - the codename index answers static rows' ids and
names

The flag war's poles, flag, key and doors are service characterdata rows
buildCharacterRef refuses (no movement columns). The index must still
give their RefObjID and their English name (#4711), and must not invent a
name for a row whose symbol has no text.

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
writeCharacterIDTextdata

A textdata folder of one characterdata table and its textdataname.txt.
================
*/
func writeCharacterIDTextdata(t *testing.T, characters, names [][]string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(file string, rows [][]string) {
		lines := make([]string, 0, len(rows))
		for _, row := range rows {
			lines = append(lines, strings.Join(row, "\t"))
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("characterdata.txt", characters)
	write("textdataname.txt", names)
	return dir
}

/*
================
TestCharacterNameByCodenameNamesStaticRows

A static service row resolves its id and its English name; a row whose
symbol has no English text, an out-of-service row and an unknown codename
give no name.
================
*/
func TestCharacterNameByCodenameNamesStaticRows(t *testing.T) {
	dir := writeCharacterIDTextdata(t,
		[][]string{
			{"1", "24135", "NPC_EVENT_FLAGWAR_MASTERKEY", "", "", "SN_NPC_EVENT_FLAGWAR_MASTERKEY"},
			{"1", "24143", "NPC_EVENT_FLAGWAR_DOOR_BLUE", "", "", "SN_NPC_UNNAMED"},
			{"0", "24144", "NPC_EVENT_FLAGWAR_DOOR_RED", "", "", "SN_NPC_EVENT_FLAGWAR_DOOR_RED"},
		},
		[][]string{
			{"1", "SN_NPC_EVENT_FLAGWAR_MASTERKEY", "", "", "", "", "", "", "Key"},
			{"1", "SN_NPC_EVENT_FLAGWAR_DOOR_RED", "", "", "", "", "", "", "Gate"},
		})
	items := NewTextdataItems(dir)

	if id, ok := items.CharacterRefObjIDByCodename("NPC_EVENT_FLAGWAR_MASTERKEY"); !ok || id != 24135 {
		t.Fatalf("key id = %d %v, want 24135", id, ok)
	}
	if name, ok := items.CharacterNameByCodename("NPC_EVENT_FLAGWAR_MASTERKEY"); !ok || name != "Key" {
		t.Fatalf("key name = %q %v, want Key", name, ok)
	}
	if id, ok := items.CharacterRefObjIDByCodename("NPC_EVENT_FLAGWAR_DOOR_BLUE"); !ok || id != 24143 {
		t.Fatalf("door id = %d %v, want 24143: a row without text keeps its id", id, ok)
	}
	for _, codename := range []string{"NPC_EVENT_FLAGWAR_DOOR_BLUE", "NPC_EVENT_FLAGWAR_DOOR_RED", "NPC_ABSENT"} {
		if name, ok := items.CharacterNameByCodename(codename); ok || name != "" {
			t.Fatalf("%s name = %q %v, want none", codename, name, ok)
		}
	}
}
