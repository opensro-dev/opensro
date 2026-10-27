/*
===========================================================================

replacement_shared_test.go - DecideReplacement against the shared truth table

The browser predicts a press only when its replay of the native
replacement rule (press-admission.ts decideReplacement) admits it. Both sides read the same
cases (testdata/buff-replacement-cases.json, canonical in the client's
tests/fixtures/native), so the two decisions cannot drift apart.

===========================================================================
*/
package statuseffect

import (
	"encoding/json"
	"os"
	"testing"
)

/*
================
sharedReplacement

One descriptor as the shared table writes it.
================
*/
type sharedReplacement struct {
	Category  uint32 `json:"category"`
	Group     uint32 `json:"group"`
	Rank      uint8  `json:"rank"`
	States    uint32 `json:"states"`
	Ovl2      uint32 `json:"ovl2"`
	Cbuf      bool   `json:"cbuf"`
	Undecided bool   `json:"undecided"`
}

/*
================
sharedReplacement.descriptor
================
*/
func (r sharedReplacement) descriptor() ReplacementDescriptor {
	return ReplacementDescriptor{Category: r.Category, Group: r.Group, Rank: r.Rank, PackedStates: r.States, Cbuf: r.Cbuf,
		Ovl2Present: r.Ovl2 != 0, Ovl2: r.Ovl2}
}

/*
================
TestDecideReplacementMatchesTheSharedTable

Every case the client decides (admit or refuse) is the server's decision
too. Unknown cases are the client's own "do not predict"; the server
decides them from state the client cannot see.
================
*/
func TestDecideReplacementMatchesTheSharedTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/buff-replacement-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Name    string               `json:"name"`
			Press   sharedReplacement    `json:"press"`
			Effects []*sharedReplacement `json:"effects"`
			Expect  string               `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	decided := 0
	for _, c := range table.Cases {
		if c.Expect == "unknown" {
			continue
		}
		var conflicts CastingConflictSnapshot
		candidates := make([]ReplacementCandidate, len(c.Effects))
		for i, effect := range c.Effects {
			candidates[i] = ReplacementCandidate{Descriptor: effect.descriptor()}
			conflicts.UpdateActive(effect.States, false)
			conflicts.UpdateActive(effect.Ovl2, false)
		}
		got := DecideReplacement(c.Press.descriptor(), candidates, true, conflicts).Allowed
		if got != (c.Expect == "admit") {
			t.Errorf("%s: server allowed=%v, table expects %s", c.Name, got, c.Expect)
		}
		decided++
	}
	if decided < 10 {
		t.Fatalf("only %d decided cases in the shared table", decided)
	}
}
