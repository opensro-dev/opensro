/*
===========================================================================

labels_test.go - answers name sessions by labels, never journal names

The journal names a session "<per-boot hex id>:<counter>"; that hex
reads as a hash, and census.go promises no hash value leaves the
package. The person and signals answers carry per-answer labels instead,
shared by the rows of one session (console#35).

===========================================================================
*/
package census

import (
	"encoding/json"
	"strings"
	"testing"
)

// journalBoot is a journal session name's per-boot hex half.
const journalBoot = "9f86d081884c7d659a2feaa0c55ad015"

/*
================
TestAnswersLabelSessionsNotJournalNames

Two spans of one journal session (a reconnect read as two spans would
share it) and one of another: the labels follow first-seen order, rows
of one session share theirs, and neither the person nor the signals
answer carries the hex id anywhere.
================
*/
func TestAnswersLabelSessionsNotJournalNames(t *testing.T) {
	now := nowMs()
	first, second := journalBoot+":1", journalBoot+":2"
	spans := []Span{
		span(first, "acct-a", "Anna", now-hourMs, 0, "ip-h", "net-h", "client-1"),
		span(second, "acct-a", "Anna", now-3*hourMs, now-2*hourMs, "ip-h", "net-h", "client-1"),
		span(first, "acct-a", "Anna", now-5*hourMs, now-4*hourMs, "ip-h", "net-h", "client-1"),
	}
	service := testService(spans, nil)

	person, found, err := service.Person("Anna")
	if err != nil || !found {
		t.Fatalf("person: found=%v err=%v", found, err)
	}
	var got []string
	for _, entry := range person.History {
		got = append(got, entry.Session)
	}
	if strings.Join(got, ",") != "Session 1,Session 2,Session 1" {
		t.Fatalf("history labels %v, want Session 1,Session 2,Session 1 newest first", got)
	}

	signals, err := service.Signals()
	if err != nil {
		t.Fatal(err)
	}
	if len(signals.Sessions) != 3 || signals.Sessions[0].Session != "Session 1" ||
		signals.Sessions[2].Session != signals.Sessions[0].Session {
		t.Fatalf("signals labels %+v", signals.Sessions)
	}

	for name, answer := range map[string]any{"person": person, "signals": signals} {
		body, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), journalBoot) {
			t.Fatalf("the %s answer carried the journal's session name: %s", name, body)
		}
	}
}
