/*
===========================================================================

session_bootstrap_bound_test.go - the unenqueued-bootstrap watchdog

A scene admission whose bootstrap (or refusal) never reaches the queue
leaves the client on a silent loading screen past its own patience.
The keepalive watchdog closes such a session at the entry bound with a
logged cause; a scene whose bootstrap was enqueued is never touched,
however long the client then takes to load it.

===========================================================================
*/
package transport

import (
	"strings"
	"testing"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"opensro.online/server/internal/testsupport/wait"
)

/*
================
bootstrapBoundSession

An attached session on a hub whose entry bound the test drives by
hand: the hub runs on a manual clock (#2445), so the watchdog sees
the bound pass only when the test advances it.
================
*/
func bootstrapBoundSession(t *testing.T, bound time.Duration) (*Session, *manualClock) {
	t.Helper()
	cfg := testCfg()
	cfg.EntryBootstrapBound = bound
	hub, clock := manualClockHub(t, cfg)
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.attach(newFakeConn(false), false, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	wait.Eventually(t, 5*time.Second, "the keepalive loop holds its ticker", func() bool {
		return clock.tickerCount() == 1
	})
	return sess, clock
}

/*
================
TestSceneBootstrapStuckClosesAtEntryBound

Begin an admission and never enqueue a bootstrap: the watchdog must
close the session and say so. Without the watchdog the session sits
in scene-loading until the idle timeout, which scene-loading pings
keep postponing forever.
================
*/
func TestSceneBootstrapStuckClosesAtEntryBound(t *testing.T) {
	hook := logtest.NewGlobal()
	sess, clock := bootstrapBoundSession(t, 150*time.Millisecond)
	sess.BeginSceneAdmission()
	// The bound passes only now, by the test's own hand.
	clock.Advance(200 * time.Millisecond)
	wait.Eventually(t, 6*time.Second, "the watchdog closes the unenqueued admission", func() bool {
		select {
		case <-sess.Done():
			return true
		default:
			return false
		}
	})
	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "scene bootstrap not enqueued") {
			found = true
		}
	}
	if !found {
		t.Fatal("the watchdog closed without its log line")
	}
}

/*
================
TestSceneBootstrapEnqueuedSurvivesEntryBound

Mark the bootstrap enqueued (the reset batch and the refusal publish
both do): the watchdog must leave the session alone past the bound,
because a slow client-side scene load is not a wedged entry.
================
*/
func TestSceneBootstrapEnqueuedSurvivesEntryBound(t *testing.T) {
	sess, clock := bootstrapBoundSession(t, 150*time.Millisecond)
	sess.BeginSceneAdmission()
	sess.MarkSceneBootstrapEnqueued()
	// Twenty bounds pass, then one more tick proves the watchdog's
	// checks for them have all run: the session must still stand.
	clock.Advance(3 * time.Second)
	clock.Advance(0)
	select {
	case <-sess.Done():
		t.Fatal("an enqueued bootstrap was watchdog-closed past the entry bound")
	default:
	}
}
