/*
===========================================================================

population_evidence_internal_test.go - the later-disable rule for evidence caps

laterDisabledCodenames flags only monsters whose every evidence row is capped
at zero. A family with any live nest keeps its per-nest zero caps.

===========================================================================
*/
package monster

import "testing"

/*
==================
TestLaterDisabledCodenamesFlagsOnlyAllZeroFamilies
==================
*/
func TestLaterDisabledCodenamesFlagsOnlyAllZeroFamilies(t *testing.T) {
	rows := map[populationEvidenceKey]populationEvidence{
		evidenceKey("MOB_ALL_ZERO", 0x8001, 1, 0, 1):      {MaxCount: 0},
		evidenceKey("MOB_ALL_ZERO", 0x8001, 2, 0, 2):      {MaxCount: 0},
		evidenceKey("MOB_MIXED", 0x8001, 3, 0, 3):         {MaxCount: 0},
		evidenceKey("MOB_MIXED", 0x8001, 4, 0, 4):         {MaxCount: 5},
		evidenceKey("MOB_LIVE", 0x6a48, 5, 0, 5):          {MaxCount: 3},
		evidenceKey("MOB_ALL_ZERO_CLON", 0x8001, 6, 0, 6): {MaxCount: 0},
	}
	got := laterDisabledCodenames(rows)
	want := map[string]bool{"MOB_ALL_ZERO": true, "MOB_ALL_ZERO_CLON": true}
	if len(got) != len(want) {
		t.Fatalf("disabled = %v, want %v", got, want)
	}
	for codename := range want {
		if !got[codename] {
			t.Fatalf("%s must be flagged: every nest is capped at zero", codename)
		}
	}
}
