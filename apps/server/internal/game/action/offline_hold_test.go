/*
===========================================================================

offline_hold_test.go - WhileOffline follows the world admission, not the session list

===========================================================================
*/
package action

import (
	"errors"
	"testing"
	"time"
)

/*
================
TestWhileOfflineRunsOnlyWithoutAnAdmission

Offline the write runs and its error comes back; admitted the write never
runs; after the session is forgotten it runs again.
================
*/
func TestWhileOfflineRunsOnlyWithoutAnAdmission(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	ran := 0
	write := func() error { ran++; return nil }

	if online, err := rt.WhileOffline(testDivision, actor.Name, write); online || err != nil || ran != 1 {
		t.Fatalf("offline: online %v, err %v, ran %d", online, err, ran)
	}
	refused := errors.New("store refused")
	if _, err := rt.WhileOffline(testDivision, actor.Name, func() error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("the write's error was lost: %v", err)
	}
	if err := rt.AdmitCharacterSession(testDivision, actor.Name, 7); err != nil {
		t.Fatal(err)
	}
	if online, err := rt.WhileOffline(testDivision, actor.Name, write); !online || err != nil || ran != 1 {
		t.Fatalf("admitted: online %v, err %v, ran %d", online, err, ran)
	}
	rt.ForgetCharacterSession(testDivision, actor.Name, 7)
	if online, _ := rt.WhileOffline(testDivision, actor.Name, write); online || ran != 2 {
		t.Fatalf("after the session ended: online %v, ran %d", online, ran)
	}
}

/*
================
TestWhileOfflineHoldsEntryOut

An entry that starts while a write runs waits for the write: the write
holds the gate entry admits under.
================
*/
func TestWhileOfflineHoldsEntryOut(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	writing, release := make(chan struct{}), make(chan struct{})
	done := make(chan bool)
	go func() {
		online, _ := rt.WhileOffline(testDivision, actor.Name, func() error {
			close(writing)
			<-release
			return nil
		})
		done <- online
	}()
	<-writing
	admitted := make(chan error, 1)
	go func() { admitted <- rt.AdmitCharacterSession(testDivision, actor.Name, 7) }()
	select {
	case err := <-admitted:
		t.Fatalf("entry was admitted while the write ran: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if online := <-done; online {
		t.Fatal("the write saw an admission that came after it")
	}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
}
