/*
===========================================================================
betamastery_test.go - publish the configured budget on every world entry

Including the per-character budget SRO_BETA_MASTERY_EARLY keeps for
characters from before the native totals (betamastery_early.go).
===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"testing"
	"time"
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

/*
================
TestBootstrapPublishesTheEarlyBetaBudgetPerCharacter

With SRO_BETA_MASTERY_EARLY on, a character created before the native
totals went live enters with the beta budget in its bootstrap; one created
since enters with none (the client's native calculation), as does every
character with the switch off. The skill window and the server's raise
check read the same rule (MasteryAllowanceOverride).
================
*/
func TestBootstrapPublishesTheEarlyBetaBudgetPerCharacter(t *testing.T) {
	before := EarlyBetaCutoff.Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	after := EarlyBetaCutoff.Add(time.Hour).Format("2006-01-02T15:04:05.000Z")
	for _, row := range []struct {
		created string
		early   bool
		want    string
	}{
		{before, true, "5000"},
		{after, true, ""},
		{before, false, ""},
		{"", true, ""},
		{"not a time", true, ""},
	} {
		c := chinaSpearman()
		c.CreatedAt = row.created
		deps := testDeps(c)
		deps.BetaMasteryEarly = row.early
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
		if got := string(view["masteryTotalOverride"]); got != row.want {
			t.Fatalf("created %q early %v: override %q, want %q", row.created, row.early, got, row.want)
		}
	}
}

/*
================
TestTheShardWideBetaBudgetWinsOverTheEarlyRule
================
*/
func TestTheShardWideBetaBudgetWinsOverTheEarlyRule(t *testing.T) {
	c := &Character{CreatedAt: EarlyBetaCutoff.Add(time.Hour).Format(time.RFC3339)}
	if MasteryAllowanceOverride(7000, true, c) != 7000 || MasteryAllowanceOverride(0, true, c) != 0 ||
		MasteryAllowanceOverride(0, true, nil) != 0 {
		t.Fatal("the shard-wide budget must apply to every character, the early one only to early characters")
	}
}

/*
================
TestBetaMasteryEarlyFromEnv
================
*/
func TestBetaMasteryEarlyFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "TRUE": true} {
		t.Setenv(EnvBetaMasteryEarly, value)
		if got, err := BetaMasteryEarlyFromEnv(); err != nil || got != want {
			t.Fatalf("%q: %v %v", value, got, err)
		}
	}
	t.Setenv(EnvBetaMasteryEarly, "maybe")
	if _, err := BetaMasteryEarlyFromEnv(); err == nil {
		t.Fatal("an invalid switch must refuse startup")
	}
}
