/*
===========================================================================

gatewait_bound_test.go - a parked publication gate abandons, never hides

The enter handler takes the publication gate before scene admission,
so a handler parked on it is invisible to the bootstrap watchdog and
the slow-entry log. Past the gate bound the handler abandons the
entry unanswered and names the wait; the client retries, and the
retry must find the queue moving again.

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestEnterWorldGateWaitBoundAbandonsThenRetryAnswers

Hold the publication gate past the bound: the handler must abandon
the entry unanswered and leave the abandonment line, and once the
gate frees, a retry on the same session must answer. Without the
bound the handler parks before scene admission and none of this is
observable - the Eventually on the log line times out instead.
================
*/
func TestEnterWorldGateWaitBoundAbandonsThenRetryAnswers(t *testing.T) {
	hook := logtest.NewGlobal()
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	authenticated := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	c := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {c}}
	gateRelease := make(chan struct{})
	var gateCalls atomic.Int32
	deps := &enterworld.Deps{
		Roster: &enterworld.Roster{}, Characters: source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		GateWaitBound:     50 * time.Millisecond,
		LockPublication: func(string, string) func() {
			if gateCalls.Add(1) == 1 {
				<-gateRelease
			}
			return func() {}
		},
		EntrySkills: func(string, string) []enterworld.EntrySkill { return []enterworld.EntrySkill{{ID: 0}} },
	}
	enterworld.RegisterEnterWorld(srv.Hub, deps)
	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test")})}
	wait.Eventually(t, 10*time.Second, "welcome", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})
	enterPayload := transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: enterPayload}
	wait.Eventually(t, 10*time.Second, "the abandoned entry to name the gate wait", func() bool {
		for _, entry := range hook.AllEntries() {
			if strings.Contains(entry.Message, "abandoned at the publication gate") {
				return true
			}
		}
		return false
	})
	// The abandoned entry stays unanswered; only a retry may answer.
	wait.Consistently(t, 200*time.Millisecond, "no enter-world answer from the abandoned entry", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpEnterWorldResult {
				return false
			}
		}
		return true
	})
	close(gateRelease)
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: enterPayload}
	wait.Eventually(t, 10*time.Second, "the retry to answer once the gate frees", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpEnterWorldResult {
				return true
			}
		}
		return false
	})
}
