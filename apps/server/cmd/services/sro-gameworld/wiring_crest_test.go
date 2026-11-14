/*
===========================================================================

wiring_crest_test.go - every shard has a crest store

A shard with no operator marks directory keeps its crests inside its
authority directory, so the native crest upload is accepted (#3454); an
operator's SRO_AGENT_MARKS_DIR still wins.

===========================================================================
*/
package main

import (
	"path/filepath"
	"testing"
)

/*
================
TestCrestMarksDirDefaultsIntoTheAuthorityDirectory
================
*/
func TestCrestMarksDirDefaultsIntoTheAuthorityDirectory(t *testing.T) {
	authority := filepath.Join("state", "shards", "s1", "authority")
	if got, want := crestMarksDir("", authority), filepath.Join(authority, crestMarksSubdir); got != want {
		t.Fatalf("unset: marks dir %q, want %q", got, want)
	}
	if got, want := crestMarksDir("  ", authority), filepath.Join(authority, crestMarksSubdir); got != want {
		t.Fatalf("blank: marks dir %q, want %q", got, want)
	}
	operator := filepath.Join("srv", "marks")
	if got := crestMarksDir(" "+operator+" ", authority); got != operator {
		t.Fatalf("operator setting: marks dir %q, want %q", got, operator)
	}
	if got := crestMarksDir("", ""); got != "" {
		t.Fatalf("no authority directory: marks dir %q, want none", got)
	}
}
