/*
===========================================================================

account_session_test.go - one account cannot fill the session registry

Every session costs an admission ticket an account can mint at will, so the
hub caps the live sessions per account. A new session evicts the account's
oldest (never refused, so a reloading player is never locked out), other
accounts are untouched, and teardown clears the index.

===========================================================================
*/
package transport

import (
	"errors"
	"testing"
)

/*
================
accountHub

A hub whose admission maps ticket "alice-N" to account alice and "bob-N" to
account bob.
================
*/
func accountHub(t *testing.T, perAccount int) *Hub {
	t.Helper()
	cfg := testCfg()
	cfg.MaxSessionsPerAccount = perAccount
	hub := newHub(cfg)
	hub.SetHelloAuth(func(ticket []byte) (AdmissionIdentity, error) {
		switch string(ticket[:3]) {
		case "ali":
			return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, nil
		case "bob":
			return AdmissionIdentity{AccountID: "bob", ShardID: "global-official"}, nil
		}
		return AdmissionIdentity{}, errors.New("unknown ticket")
	})
	t.Cleanup(func() {
		for _, session := range hub.Sessions() {
			hub.closeSession(session, nil)
		}
	})
	return hub
}

/*
================
admit

Runs one fresh HELLO and returns the session it created.
================
*/
func admit(t *testing.T, hub *Hub, ticket string) *Session {
	t.Helper()
	before := map[uint64]bool{}
	for _, session := range hub.Sessions() {
		before[session.ID] = true
	}
	hub.AcceptConn(helloConn(nil, []byte(ticket)))
	for _, session := range hub.Sessions() {
		if !before[session.ID] {
			return session
		}
	}
	t.Fatalf("HELLO %q created no session", ticket)
	return nil
}

func TestAccountSessionLimitEvictsTheOldestOfThatAccountOnly(t *testing.T) {
	hub := accountHub(t, 2)
	bob := admit(t, hub, "bob-1")
	first := admit(t, hub, "alice-1")
	second := admit(t, hub, "alice-2")
	if first.Evicted() || second.Evicted() {
		t.Fatal("sessions within the limit were evicted")
	}
	third := admit(t, hub, "alice-3")
	if !first.Evicted() {
		t.Fatal("the account's oldest session survived past the limit")
	}
	if second.Evicted() || third.Evicted() {
		t.Fatal("a session inside the limit was evicted")
	}
	if bob.Evicted() {
		t.Fatal("another account's session was evicted")
	}
	if got := hub.Metrics().AccountSessionEvictions; got != 1 {
		t.Fatalf("account session evictions = %d, want 1", got)
	}
}

func TestAccountSessionLimitCountsOnlyLiveSessions(t *testing.T) {
	hub := accountHub(t, 2)
	admit(t, hub, "alice-1")
	admit(t, hub, "alice-2")
	admit(t, hub, "alice-3") // evicts alice-1, which may still be draining
	admit(t, hub, "alice-4") // must evict exactly one more, not two
	if got := hub.Metrics().AccountSessionEvictions; got != 2 {
		t.Fatalf("account session evictions = %d, want 2", got)
	}
	live := 0
	for _, session := range hub.Sessions() {
		if !session.Evicted() {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("live sessions = %d, want the limit 2", live)
	}
}

func TestAccountSessionIndexClearsOnTeardown(t *testing.T) {
	hub := accountHub(t, 2)
	session := admit(t, hub, "alice-1")
	hub.closeSession(session, nil)
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if len(hub.accountSessions) != 0 || len(hub.sessionAccount) != 0 {
		t.Fatalf("account index kept %d accounts, %d sessions", len(hub.accountSessions), len(hub.sessionAccount))
	}
}
