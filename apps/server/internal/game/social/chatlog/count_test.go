/*
===========================================================================

count_test.go - lines per character over a window

===========================================================================
*/
package chatlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
================
TestCountByCharacter

Counts fold case like the searches do, the window bounds both ends,
and a torn line counts as skipped, never as silence.
================
*/
func TestCountByCharacter(t *testing.T) {
	dir := t.TempDir()
	day := "2026-11-06"
	lines := `{"ts":"2026-11-06T10:00:00Z","account":"a1","character":"Hero","channel":"all","muted":false,"text":"one"}
{"ts":"2026-11-06T11:00:00Z","account":"a1","character":"HERO","channel":"all","muted":false,"text":"two"}
{"ts":"2026-11-06T12:00:00Z","account":"a2","character":"Mage","channel":"all","muted":false,"text":"three"}
{"ts":"2026-11-06T13:00:00Z","account":"a1","character":"Hero","channel":"all","muted":false,"text":"four"
`
	if err := os.WriteFile(filepath.Join(dir, FilePrefix+day+FileSuffix), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	counts, skipped, err := CountByCharacter(dir, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if counts["hero"] != 2 || counts["mage"] != 1 {
		t.Fatalf("counts: got %v, want hero 2 and mage 1", counts)
	}
	if skipped != 1 {
		t.Fatalf("skipped: got %d, want 1 torn line", skipped)
	}

	counts, _, err = CountByCharacter(dir, from, from.Add(10*time.Hour+30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts["hero"] != 1 {
		t.Fatalf("windowed counts: got %v, want only the first hero line", counts)
	}
}

/*
================
TestCountByCharacterMissingDirectory
================
*/
func TestCountByCharacterMissingDirectory(t *testing.T) {
	counts, skipped, err := CountByCharacter(filepath.Join(t.TempDir(), "absent"), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 || skipped != 0 {
		t.Fatalf("missing directory: got %v and %d skipped, want an empty count", counts, skipped)
	}
}
