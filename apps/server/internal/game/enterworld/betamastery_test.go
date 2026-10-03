/*
===========================================================================
betamastery_test.go - publish the configured budget on every world entry
===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"testing"
)

/*
================
TestBootstrapMasteryOverride

The override belongs to this server session, not the persisted character.
Omitting it keeps older/native servers compatible with the browser.
================
*/
func TestBootstrapMasteryOverride(t *testing.T) {
	for _, limit := range []int64{0, 5000} {
		c := chinaSpearman()
		deps := testDeps(c)
		deps.MasteryTotalOverride = limit
		result := Build(deps, BootstrapRequest{CharacterName: c.Name})
		if result.NativeResult != nativeResultSuccess {
			t.Fatal(result.Reason)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]json.RawMessage
		if err := json.Unmarshal(data, &view); err != nil {
			t.Fatal(err)
		}
		value, present := view["masteryTotalOverride"]
		if limit == 0 && present {
			t.Fatal("native bootstrap carries override")
		}
		if limit != 0 && string(value) != "5000" {
			t.Fatalf("wrong override %s", value)
		}
	}
}
