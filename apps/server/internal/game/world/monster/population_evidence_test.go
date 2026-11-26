/*
===========================================================================

population_evidence_test.go - the population evidence row contract

The shipped evidence rows carry the nest's authored spawn kind as the
23rd column; rows written before it (the ISRO-R supplement) read as
kind 0. These tests pin both shapes so a regenerated table cannot
silently lose the kind or shift a column.

===========================================================================
*/
package monster

import "testing"

const evidenceRowBase = "MOB_EVIDENCE_TEST\t25000\t10\t20\t30\t100\t50\t10\t5\t9\t3\t1\t1\t200\t7\t1\t0\t0\t0\t0\t0\t90"

/*
================
TestPopulationEvidenceSpawnType

A 23-column row keeps its authored spawn kind; a legacy 22-column row
reads as kind 0.
================
*/
func TestPopulationEvidenceSpawnType(t *testing.T) {
	key := evidenceKey("MOB_EVIDENCE_TEST", 25000, 10, 20, 30)

	rows := mustLoadPopulationEvidence(evidenceRowBase + "\t1")
	if got := rows[key].SpawnType; got != 1 {
		t.Fatalf("23-column spawn kind = %d, want 1", got)
	}

	rows = mustLoadPopulationEvidence(evidenceRowBase)
	if got := rows[key].SpawnType; got != 0 {
		t.Fatalf("22-column spawn kind = %d, want 0", got)
	}
}
