/*
===========================================================================

census_test.go - entered sessions reach the census with their wire facts

The census observer (#2716) is the history stream's second reader: a
world entry carries the attaching connection's real client address and
User-Agent, and the session's end reaches it too. With no observer
installed (the native posture) nothing in this file would observe a
thing, which is the flag-off shape every other transport test runs.
===========================================================================
*/
package transport

import "testing"

/*
================
TestCensusReceivesEnteredFactsAndEnd
================
*/
func TestCensusReceivesEnteredFactsAndEnd(t *testing.T) {
	h := newHub(testCfg())
	census := &recordingHistory{}
	h.Census = census
	s, err := h.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	fc.kind = "websocket"
	fc.addr = "203.0.113.9"
	fc.userAgent = "TestBrowser/1.0 (Unit Test)"
	if err := s.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	s.BindCharacter("global-official", "Hero", 42)
	if !s.TryMarkWorldReady() {
		t.Fatal("world entry did not land")
	}
	entered, ok := census.byKind("world_entered")
	if !ok {
		t.Fatal("census saw no world entry")
	}
	if entered.Character != "Hero" || entered.Transport != "websocket" ||
		entered.ClientAddr != "203.0.113.9" || entered.UserAgent != "TestBrowser/1.0 (Unit Test)" {
		t.Fatalf("entered event = %+v", entered)
	}
	h.closeSession(s, nil)
	if _, ok := census.byKind("ended"); !ok {
		t.Fatal("census saw no session end")
	}
}
