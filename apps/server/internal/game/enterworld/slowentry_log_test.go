/*
===========================================================================

slowentry_log_test.go - a slow enter world names the phase it waited in

The enter handler times its publication-gate wait against its total
duration and logs both once past the slow threshold, so a multi-second
entry is visible in the server log instead of only in a client report.
The test holds the gate on a channel past a threshold lowered through
deps instead of sleeping: while the gate is held the entry provably
cannot answer, and the held window is what the gate wait must name.

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"strings"
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
TestSlowEnterWorldLogsItsPhases

Hold the publication gate on a channel: while it is held the entry
cannot answer, and once the test releases it the handler must leave
one log line naming the wait it sat in. The no-answer window runs
entirely inside the hold, so the recorded gate wait covers it and
trips the lowered threshold by construction, not by timing luck.
Completed entries are a histogram bucket and nothing more, so
without this line a multi-second entry is invisible until a client
report reconstructs it. Removing the phase log fails this test.
================
*/
func TestSlowEnterWorldLogsItsPhases(t *testing.T) {
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
	gateHeld := make(chan struct{})
	gateRelease := make(chan struct{})
	deps := &enterworld.Deps{
		Roster: &enterworld.Roster{}, Characters: source,
		Items:              enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
		ResolveDivisionID:  enterworld.DevResolveDivisionIDFromCatalog(source),
		SlowEnterThreshold: 50 * time.Millisecond,
		LockPublication: func(string, string) func() {
			close(gateHeld)
			<-gateRelease
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
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))}
	wait.Eventually(t, 10*time.Second, "the entry parked in the publication gate", func() bool {
		select {
		case <-gateHeld:
			return true
		default:
			return false
		}
	})
	// The no-answer window runs inside the hold, so the gate wait the
	// handler records afterwards covers it: past the 50 ms threshold
	// by construction, not by wall-clock luck.
	wait.Consistently(t, 150*time.Millisecond, "no enter-world answer while the gate is held", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpEnterWorldResult {
				return false
			}
		}
		return true
	})
	close(gateRelease)
	wait.Eventually(t, 10*time.Second, "the released entry to answer", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpEnterWorldResult {
				return true
			}
		}
		return false
	})
	wait.Eventually(t, 10*time.Second, "the slow entry to log its phases", func() bool {
		for _, entry := range hook.AllEntries() {
			if !strings.Contains(entry.Message, "slow enter world") {
				continue
			}
			gate, ok := entry.Data["gate_ms"].(int64)
			if ok && gate >= 50 {
				return true
			}
		}
		return false
	})
}
