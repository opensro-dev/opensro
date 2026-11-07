/*
===========================================================================

signals_test.go - bot signals on synthetic sessions

Long unbroken spans, twenty-of-twenty-four-hour presence and hours
of chat silence fire from fixture spans and counts; a chatty player
fires none, and damaged chat evidence silences the signal instead
of the player.

===========================================================================
*/
package census

import (
	"errors"
	"testing"
)

// signalKinds is a session's fired signal kinds.
func signalKinds(entry SessionSignals) map[string]float64 {
	kinds := map[string]float64{}
	for _, signal := range entry.Signals {
		kinds[signal.Kind] = signal.Value
	}
	return kinds
}

// sessionByID finds one session in the answer.
func sessionByID(answer Signals, session string) SessionSignals {
	for _, entry := range answer.Sessions {
		if entry.Session == session {
			return entry
		}
	}
	return SessionSignals{Session: "missing"}
}

/*
================
TestSignalsOnSyntheticSessions
================
*/
func TestSignalsOnSyntheticSessions(t *testing.T) {
	now := nowMs()
	spans := []Span{
		// Nine unbroken hours, but the character talks: long only.
		span("s-long", "acct-bot", "Grinder", now-9*hourMs, 0, "ip-a", "net-a", "client-a"),
		// Twenty-one of the last twenty-four hours across two spans,
		// and not a word said: always-on, long, silent.
		span("s-camp-1", "acct-camp", "Camper", now-21*hourMs, now-hourMs, "ip-b", "net-b", "client-b"),
		span("s-camp-2", "acct-camp", "Camper", now-hourMs, 0, "ip-b", "net-b", "client-b"),
		// Three ordinary hours with chat: nothing fires.
		span("s-human", "acct-human", "Talker", now-4*hourMs, now-hourMs, "ip-c", "net-c", "client-c"),
	}
	chat := func(int64, int64) (map[string]int, error) {
		return map[string]int{"grinder": 4, "talker": 12}, nil
	}
	answer, err := testService(spans, chat).Signals()
	if err != nil {
		t.Fatal(err)
	}
	if !answer.ChatOK {
		t.Fatal("chat read clean but ChatOK is false")
	}
	if len(answer.Sessions) != 4 {
		t.Fatalf("sessions: got %d, want 4", len(answer.Sessions))
	}
	if !answer.Sessions[0].Open {
		t.Fatal("open sessions must sort first")
	}

	long := signalKinds(sessionByID(answer, "s-long"))
	if long["long-session"] != 9 {
		t.Fatalf("long session: got %v, want long-session 9", long)
	}
	if _, fired := long["chat-silent"]; fired {
		t.Fatal("a talking character read as chat-silent")
	}
	if _, fired := long["always-on"]; fired {
		t.Fatal("nine hours read as always-on")
	}

	camp := signalKinds(sessionByID(answer, "s-camp-1"))
	if camp["always-on"] != 21 || camp["long-session"] != 20 {
		t.Fatalf("camper's long span: got %v, want always-on 21 and long-session 20", camp)
	}
	if _, fired := camp["chat-silent"]; !fired {
		t.Fatal("twenty silent hours did not fire chat-silent")
	}
	campOpen := signalKinds(sessionByID(answer, "s-camp-2"))
	if _, fired := campOpen["always-on"]; !fired {
		t.Fatal("the camper's open hour lost the account's always-on")
	}
	if _, fired := campOpen["chat-silent"]; fired {
		t.Fatal("a one-hour span fired chat-silent")
	}

	human := signalKinds(sessionByID(answer, "s-human"))
	if len(human) != 0 {
		t.Fatalf("an ordinary chatty session fired %v", human)
	}
}

/*
================
TestSignalsWithoutChatEvidence

A failed chat count answers ChatOK false and fires no chat-silent
anywhere: unknown is not silent.
================
*/
func TestSignalsWithoutChatEvidence(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Silent", now-5*hourMs, now-hourMs, "ip-a", "net-a", "client-a"),
	}
	chat := func(int64, int64) (map[string]int, error) {
		return nil, errors.New("log unreadable")
	}
	answer, err := testService(spans, chat).Signals()
	if err != nil {
		t.Fatal(err)
	}
	if answer.ChatOK {
		t.Fatal("a failed chat count read as OK")
	}
	for _, entry := range answer.Sessions {
		for _, signal := range entry.Signals {
			if signal.Kind == "chat-silent" {
				t.Fatal("chat-silent fired without chat evidence")
			}
		}
	}

	noChat, err := testService(spans, nil).Signals()
	if err != nil {
		t.Fatal(err)
	}
	if noChat.ChatOK {
		t.Fatal("no chat source read as OK")
	}
}
