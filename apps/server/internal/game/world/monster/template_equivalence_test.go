/*
===========================================================================

template_equivalence_test.go - the shipped template is the evidence's template

The tables moved from the server source to the operator's source-data. The
template built from them must equal the one the embedded tables built: the
counts and the digests below were measured on that earlier build and match
this one. Refresh them only when the source-data changes on purpose.

===========================================================================
*/
package monster_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	. "opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/monstertest"
)

const (
	// shippedNestCount and shippedMatchCount are the template's nests and
	// the nests the evidence matched.
	shippedNestCount  = 8753
	shippedMatchCount = 8458
	// shippedTemplateDigest is the sha256 of the template's nests as JSON.
	shippedTemplateDigest = "7994889353a76d911fcae7f029dd75775b74d259fd5c158558feabaa6bb7d294"
	// shippedSummonCount and shippedSummonDigest are the summon tactics
	// codes and the sha256 of the table as JSON (keys sorted).
	shippedSummonCount  = 398
	shippedSummonDigest = "b73bb3b5b11368239607eef74df8fc585fff8399e5777a793af773cc7ef476ac"
)

/*
================
jsonDigest
================
*/
func jsonDigest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

/*
================
TestShippedTemplateEvidenceEquivalence

The shipped textdata with the operator's evidence builds the same template
and summon table as the embedded tables did. Skips without the textdata or
the evidence; fails under SRO_REQUIRE_GAME_DATA=1.
================
*/
func TestShippedTemplateEvidenceEquivalence(t *testing.T) {
	evidence := monstertest.Evidence(t)
	template := monstertest.Template(t, gamedatatest.TextdataDir(t))
	if len(template.Nests) != shippedNestCount || template.EvidenceMatches != shippedMatchCount {
		t.Fatalf("template nests %d matches %d, want %d and %d", len(template.Nests), template.EvidenceMatches,
			shippedNestCount, shippedMatchCount)
	}
	if got := jsonDigest(t, template.Nests); got != shippedTemplateDigest {
		t.Fatalf("template digest %s, want %s (a nest or its fields changed)", got, shippedTemplateDigest)
	}
	summon := evidence.SummonTactics()
	if len(summon) != shippedSummonCount {
		t.Fatalf("summon tactics codes %d, want %d", len(summon), shippedSummonCount)
	}
	if got := jsonDigest(t, map[string][2][]SummonTactics(summon)); got != shippedSummonDigest {
		t.Fatalf("summon digest %s, want %s (a tactics row changed)", got, shippedSummonDigest)
	}
}
