/*
===========================================================================

evicted_resume_test.go - a resume of an evicted session is told why (#2146)

A session evicted by a newer login of the same character used to come
back with its token, find it unknown, and get a fresh session that
evicted the winner in turn: the two-device ping-pong behind most live
"resume token presented but unknown" lines on 2026-11-03. These pin the
contract that replaced it: the evicted token is answered with its BYE.

===========================================================================
*/
package transport

import (
	"testing"
	"time"
)

const evictedResumeKey = "d1:char"

/*
================
evictWhileDetached

A loser session bound to the key, then replaced by a winner while the
loser is detached (in its resume grace, so no BYE can reach its client).
Returns both.
================
*/
func evictWhileDetached(t *testing.T, hub *Hub) (*Session, *Session) {
	t.Helper()
	loser, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	winner, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(winner, nil) })
	if _, replaced := hub.BindExclusive(evictedResumeKey, loser); replaced {
		t.Fatal("the first bind replaced a session")
	}
	if old, replaced := hub.BindExclusive(evictedResumeKey, winner); !replaced || old != loser {
		t.Fatal("the second bind did not evict the loser")
	}
	return loser, winner
}

/*
================
presentToken

A HELLO that resumes token at stream position 0, as the client's
reconnect loop sends it. Returns what the hub wrote back.
================
*/
func presentToken(t *testing.T, hub *Hub, token [ResumeTokenLen]byte) []Frame {
	t.Helper()
	connection := newFakeConn(false)
	connection.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{
		ResumeToken: token[:], AdmissionToken: testHelloAdmissionToken, HasPosition: true,
	})}
	hub.AcceptConn(connection)
	return waitWritten(t, connection, 1)
}

/*
================
TestEvictedResumeIsToldItWasReplaced
================
*/
func TestEvictedResumeIsToldItWasReplaced(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	loser, winner := evictWhileDetached(t, hub)

	written := presentToken(t, hub, loser.resumeToken)
	if written[0].Opcode != OpBye || len(written[0].Payload) != 1 || written[0].Payload[0] != ByeReasonReplaced {
		t.Fatalf("the evicted resume got op 0x%04X payload % X, want BYE(Replaced)", written[0].Opcode, written[0].Payload)
	}
	if sessions := hub.Sessions(); len(sessions) != 1 || sessions[0] != winner {
		t.Fatalf("live sessions = %d, want only the winner: no fresh session for the loser", len(sessions))
	}
	if bound, ok := hub.BoundSession(evictedResumeKey); !ok || bound != winner {
		t.Fatal("the winner lost its binding to the evicted resume")
	}
	metrics := hub.Metrics()
	if metrics.ResumeAttempted != 1 || metrics.ResumeEnded != 1 || metrics.ResumeFresh != 0 {
		t.Fatalf("resume metrics = attempted %d ended %d fresh %d, want 1/1/0",
			metrics.ResumeAttempted, metrics.ResumeEnded, metrics.ResumeFresh)
	}
}

/*
================
TestEvictedTokenIsForgottenAfterItsWindow

Past endedTokenGraces grace periods the token is an ordinary unknown one
again (a fresh session), and expired records leave the map.
================
*/
func TestEvictedTokenIsForgottenAfterItsWindow(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	loser, _ := evictWhileDetached(t, hub)

	if _, ok := hub.endedResume(loser.resumeToken, time.Now()); !ok {
		t.Fatal("the evicted token was not remembered at teardown")
	}
	later := time.Now().Add(endedTokenGraces*hub.cfg.GracePeriod + time.Second)
	if _, ok := hub.endedResume(loser.resumeToken, later); ok {
		t.Fatal("the evicted token was still remembered after its window")
	}

	// The next eviction prunes the expired record.
	hub.mu.Lock()
	hub.ended[loser.resumeToken] = endedToken{reason: ByeReasonReplaced, until: time.Now().Add(-time.Second)}
	hub.mu.Unlock()
	other, _ := evictWhileDetachedKey(t, hub, "d1:other")
	hub.mu.RLock()
	_, stale := hub.ended[loser.resumeToken]
	_, fresh := hub.ended[other.resumeToken]
	hub.mu.RUnlock()
	if stale || !fresh {
		t.Fatalf("ended records: expired kept %v, new kept %v; want false/true", stale, fresh)
	}

	written := presentToken(t, hub, loser.resumeToken)
	if written[0].Opcode != OpWelcome {
		t.Fatalf("an expired evicted token got op 0x%04X, want a fresh WELCOME", written[0].Opcode)
	}
	for _, s := range hub.Sessions() {
		defer hub.closeSession(s, nil)
	}
}

/*
================
TestAccountEvictionResumeIsToldUnauthorized

An account eviction (staff block) ends its sessions with Unauthorized;
their resume hears the same reason.
================
*/
func TestAccountEvictionResumeIsToldUnauthorized(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	victim, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	hub.admitAccountSession("test-account", victim)
	if n := hub.EvictAccount("test-account"); n != 1 {
		t.Fatalf("EvictAccount evicted %d sessions, want 1", n)
	}
	written := presentToken(t, hub, victim.resumeToken)
	if written[0].Opcode != OpBye || written[0].Payload[0] != ByeReasonUnauthorized {
		t.Fatalf("the evicted account's resume got op 0x%04X payload % X, want BYE(Unauthorized)", written[0].Opcode, written[0].Payload)
	}
	if n := len(hub.Sessions()); n != 0 {
		t.Fatalf("live sessions = %d, want 0", n)
	}
}

/*
================
evictWhileDetachedKey

evictWhileDetached on another bind key.
================
*/
func evictWhileDetachedKey(t *testing.T, hub *Hub, key string) (*Session, *Session) {
	t.Helper()
	loser, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	winner, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(winner, nil) })
	hub.BindExclusive(key, loser)
	if old, replaced := hub.BindExclusive(key, winner); !replaced || old != loser {
		t.Fatal("the second bind did not evict the loser")
	}
	return loser, winner
}
