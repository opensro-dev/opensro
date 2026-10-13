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

/*
================
TestDegradedReportsGroupBySiteApartFromFailures

The client's non-fatal reports group by their site code with the newest
as the example, and never count among the failure groups.
================
*/
func TestDegradedReportsGroupBySiteApartFromFailures(t *testing.T) {
	j := testJournal(t)
	for _, e := range []Event{
		{ID: "d1", At: 1000, Account: "a", Kind: "client_incident", Category: "degraded", Code: "item-use-visual-unknown-ref",
			Message: "older", Opcode: "0x3449", Build: "b1", Fields: map[string]string{"payload": "aa"}},
		{ID: "d2", At: 2000, Account: "b", Kind: "client_incident", Category: "degraded", Code: "item-use-visual-unknown-ref",
			Message: "newest", Opcode: "0x3449", Build: "b1", Fields: map[string]string{"payload": "cb860100cb0e0000"}},
		{ID: "d3", At: 1500, Account: "a", Kind: "client_incident", Category: "degraded", Code: "other-site", Message: "x"},
		{ID: "f1", At: 1200, Account: "a", Kind: "client_incident", Category: "software", Code: "packet_application_failed", Message: "fatal"},
	} {
		if err := j.write(e); err != nil {
			t.Fatal(err)
		}
	}
	data, err := j.Query(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	degraded := data["degraded"].([]map[string]any)
	if len(degraded) != 2 || degraded[0]["code"] != "item-use-visual-unknown-ref" {
		t.Fatalf("degraded groups = %+v", degraded)
	}
	row := degraded[0]
	example := row["example"].(map[string]any)
	if row["count"] != int64(2) || row["players"] != int64(2) || row["first"] != int64(1000) || row["last"] != int64(2000) ||
		example["message"] != "newest" || example["opcode"] != "0x3449" || example["payload"] != "cb860100cb0e0000" {
		t.Fatalf("degraded row = %+v", row)
	}
	for _, group := range data["groups"].([]map[string]any) {
		if group["code"] == "item-use-visual-unknown-ref" || group["code"] == "other-site" {
			t.Fatalf("a degraded report counted as a failure: %+v", group)
		}
	}
}
