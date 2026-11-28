/*
===========================================================================

wiring_hwan_test.go - tests for wiring_hwan.go

===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestHwanQuestsBuiltReadsTheQuestRows
================
*/
func TestHwanQuestsBuiltReadsTheQuestRows(t *testing.T) {
	dir := t.TempDir()
	if hwanQuestsBuilt(dir) {
		t.Fatal("no file counted as built")
	}
	write := func(data []byte) {
		if err := os.WriteFile(filepath.Join(dir, "questdata.txt"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write([]byte("1\t360\tQNO_EU_HWAN_1_1\t95\n"))
	if hwanQuestsBuilt(dir) {
		t.Fatal("the European row alone counted as the chain")
	}
	write([]byte("1\t349\tQNO_CH_HWAN_1_1\t95\n"))
	if !hwanQuestsBuilt(dir) {
		t.Fatal("the Chinese row was not found")
	}
	write([]byte{'Q', 0, 'N', 0, 'O', 0, '_', 0, 'C', 0, 'H', 0, '_', 0, 'H', 0, 'W', 0, 'A', 0, 'N', 0, '_', 0, '1', 0, '_', 0, '1', 0})
	if !hwanQuestsBuilt(dir) {
		t.Fatal("the UTF-16 row was not found")
	}
}

/*
================
TestHwanReverseMapExtrasFollowTheQuestRows

The NPC homes need the quest rows. The tomb adds no point of its own
(the imported table's cave flag reaches it), whatever SRO_QINSHI_TOMB says.
================
*/
func TestHwanReverseMapExtrasFollowTheQuestRows(t *testing.T) {
	names := func(dir string) string {
		out := ""
		for _, point := range hwanReverseMapExtras(dir) {
			out += point.Name + ";"
		}
		return out
	}
	bare, built := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(built, "questdata.txt"), []byte("1\t349\tQNO_CH_HWAN_1_1\t95\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tomb := range []string{"off", "on"} {
		t.Setenv("SRO_QINSHI_TOMB", tomb)
		if got := names(bare); got != "" {
			t.Fatalf("SRO_QINSHI_TOMB=%s, no quests: %q", tomb, got)
		}
		if got := names(built); got != "Exorcist's Home;Sunset Witch;" {
			t.Fatalf("SRO_QINSHI_TOMB=%s, quest rows: %q", tomb, got)
		}
	}
}
