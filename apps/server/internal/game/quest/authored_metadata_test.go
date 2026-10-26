/*
===========================================================================

authored_metadata_test.go - compilation availability follows registration

The derived metadata must match the actual loader composition. A source
literal, missing registration or stale snapshot cannot unlock a successor.
Required curated definitions retain their fail-closed media contract.

===========================================================================
*/

package quest

import (
	"slices"
	"strings"
	"testing"
)

/*
================
TestAuthoredMetadataMatchesRegisteredComposition
================
*/
func TestAuthoredMetadataMatchesRegisteredComposition(t *testing.T) {
	required, additional, err := authoredQuestSpecs()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAuthoredQuestMetadata(authoredCatalogMetadata, required, additional); err != nil {
		t.Fatal(err)
	}
	codes, err := AuthoredQuestCodenames()
	if err != nil || len(codes) == 0 || !slices.IsSorted(codes) {
		t.Fatalf("registered metadata=%v/%v", codes, err)
	}
}

/*
================
TestAuthoredMetadataRejectsUnregisteredAndStaleNames
================
*/
func TestAuthoredMetadataRejectsUnregisteredAndStaleNames(t *testing.T) {
	const registered = "QNO_REGISTERED"
	required := []QuestSpec{{Codename: registered}}
	for _, raw := range []string{
		`[]`, `null`, `{}`, `not json`, `[1]`, `[""]`,
		`["QNO_REGISTERED", "QNO_REGISTERED"]`,
		`["QNO_REGISTERED", "QNO_UNREGISTERED"]`,
		`["QNO_UNREGISTERED"]`,
		`["QNO_REGISTERED"] []`,
	} {
		t.Run(raw, func(t *testing.T) {
			if err := validateAuthoredQuestMetadata(raw, required, nil); err == nil {
				t.Fatal("invalid registration metadata admitted a compiled successor")
			}
		})
	}
	if err := validateAuthoredQuestMetadata(`["QNO_REGISTERED"]`, nil, nil); err == nil {
		t.Fatal("a metadata name without its registered definition was accepted")
	}
	if err := validateAuthoredQuestMetadata(`["QNO_REGISTERED"]`, required, nil); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestAuthoredMetadataExporterUsesValuesAndRejectsDuplicateRegistration
================
*/
func TestAuthoredMetadataExporterUsesValuesAndRejectsDuplicateRegistration(t *testing.T) {
	const prefix = "QNO_REGISTERED"
	required := []QuestSpec{{Codename: prefix + "_B"}}
	additional := []QuestSpec{{Codename: prefix + "_A"}}
	codes, err := authoredQuestCodenames(required, additional)
	if err != nil || !slices.Equal(codes, []string{prefix + "_A", prefix + "_B"}) {
		t.Fatalf("constant-built registration=%v/%v", codes, err)
	}
	if _, err := authoredQuestCodenames(required, required); err == nil {
		t.Fatal("two actual registrations silently shared a codename")
	}
	if _, err := authoredQuestCodenames([]QuestSpec{{}}, nil); err == nil {
		t.Fatal("an empty registered codename was exported")
	}
}

/*
================
TestAuthoredMetadataDoesNotFilterRequiredCuratedDefinitions
================
*/
func TestAuthoredMetadataDoesNotFilterRequiredCuratedDefinitions(t *testing.T) {
	catalog := writeTestCatalog(t, true)
	catalog.Len()
	missing := curatedQuestSpecs[0].Codename
	delete(catalog.rows, missing)
	if _, err := LoadDefinitions(catalog, fakeItems{}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing required curated row was filtered: %v", err)
	}
}
