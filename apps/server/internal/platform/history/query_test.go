/*
===========================================================================

query_test.go - operator filters keep SQL-looking values literal

Exercise event pages and both aggregate queries through the public query path.

===========================================================================
*/
package history

import "testing"

/*
================
TestQueryFiltersKeepSQLLookingValuesLiteral
================
*/
func TestQueryFiltersKeepSQLLookingValuesLiteral(t *testing.T) {
	const value = "x' OR 1=1 -- %_"
	j := testJournal(t)
	for _, e := range []Event{
		{ID: value, At: 1000, Account: value, Character: value, Session: value, Kind: value, Category: value, Build: value, Opcode: value, Message: value, Level: "error", Code: "literal"},
		{ID: "other", At: 2000, Account: "other", Character: "other", Session: "other", Kind: "other", Category: "software", Build: "other", Opcode: "other", Message: "other", Level: "error", Code: "other"},
	} {
		if err := j.write(e); err != nil {
			t.Fatal(err)
		}
	}
	for name, filter := range map[string]Filter{
		"account":   {Account: value},
		"character": {Character: value},
		"session":   {Session: value},
		"incident":  {Incident: value},
		"category":  {Category: value},
		"kind":      {Kind: value},
		"build":     {Build: value},
		"opcode":    {Opcode: value},
		"search":    {Search: value},
		"combined":  {Account: value, Character: value, Session: value, Incident: value, Category: value, Kind: value, Build: value, Opcode: value, Search: value, From: 1000, To: 1000},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := j.Query(filter)
			if err != nil {
				t.Fatal(err)
			}
			events := data["events"].([]EventRow)
			if len(events) != 1 || events[0].ID != value {
				t.Fatalf("filter widened or lost literal match: %+v", events)
			}
			groups := data["groups"].([]map[string]any)
			if len(groups) != 1 || groups[0]["count"] != int64(1) {
				t.Fatalf("aggregate filter widened: %+v", groups)
			}
			counts := groups[0]["buildCounts"].(map[string]int64)
			if len(counts) != 1 || counts[value] != 1 {
				t.Fatalf("build aggregate filter widened: %+v", counts)
			}
			filter.Before = events[0].Sequence
			page, err := j.Query(filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(page["events"].([]EventRow)) != 0 || len(page["groups"].([]map[string]any)) != 1 {
				t.Fatalf("cursor changed aggregate scope: %+v", page)
			}
		})
	}
}

/*
================
TestQueryRefusedLoginAccountIsLiteralAndCaseInsensitive
================
*/
func TestQueryRefusedLoginAccountIsLiteralAndCaseInsensitive(t *testing.T) {
	j := testJournal(t)
	put(t, j, Event{Kind: "login_refused", Fields: map[string]string{"claimedAccount": "Player' OR 1=1 --"}})
	put(t, j, Event{Kind: "login_refused", Fields: map[string]string{"claimedAccount": "other"}})
	data, err := j.Query(Filter{Account: "PLAYER' OR 1=1 --"})
	if err != nil {
		t.Fatal(err)
	}
	events := data["events"].([]EventRow)
	if len(events) != 1 || events[0].Fields["claimedAccount"] != "Player' OR 1=1 --" {
		t.Fatalf("claimed account filter widened or lost case folding: %+v", events)
	}
}
