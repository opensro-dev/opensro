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

An attached session on a hub whose entry bound is short enough for a
test to watch the watchdog fire.
================
*/
func bootstrapBoundSession(t *testing.T, bound time.Duration) *Session {
	t.Helper()
	cfg := testCfg()
	cfg.EntryBootstrapBound = bound
	hub := newHub(cfg)
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.attach(newFakeConn(false), false, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	return sess
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
	sess := bootstrapBoundSession(t, 150*time.Millisecond)
	sess.BeginSceneAdmission()
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
	sess := bootstrapBoundSession(t, 150*time.Millisecond)
	sess.BeginSceneAdmission()
	sess.MarkSceneBootstrapEnqueued()
	wait.Consistently(t, 3*time.Second, "an enqueued bootstrap is never watchdog-closed", func() bool {
		select {
		case <-sess.Done():
			return false
		default:
			return true
		}
	})
}
