package transport

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

var testHelloAdmissionToken = []byte("test-hello-admission-ticket")

func allowTestHelloAdmission(hub *Hub) {
	hub.SetHelloAuth(func(token []byte) (AdmissionIdentity, error) {
		if !bytes.Equal(token, testHelloAdmissionToken) {
			return AdmissionIdentity{}, errors.New("unexpected test admission ticket")
		}
		return AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
}

func helloConn(resumeToken, admissionToken []byte) *fakeConn {
	connection := newFakeConn(false)
	connection.inbound <- Frame{
		Opcode: OpHello,
		Payload: EncodeHello(Hello{
			ResumeToken:    resumeToken,
			AdmissionToken: admissionToken,
		}),
	}
	return connection
}

func TestHelloBindsIdentityBeforeSessionAdmission(t *testing.T) {
	hub := newHub(testCfg())
	ticket := []byte("valid-one-use-ticket")
	hub.SetHelloAuth(func(got []byte) (AdmissionIdentity, error) {
		if !bytes.Equal(got, ticket) {
			return AdmissionIdentity{}, errors.New("unexpected ticket")
		}
		return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, nil
	})
	connection := helloConn(nil, ticket)
	hub.AcceptConn(connection)

	sessions := hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	session := sessions[0]
	t.Cleanup(func() { hub.closeSession(session, nil) })
	accountID, shardID, ok := session.AdmissionIdentity()
	if !ok || accountID != "alice" || shardID != "global-official" {
		t.Fatalf("admission identity = (%q, %q, %v)", accountID, shardID, ok)
	}
	written := waitWritten(t, connection, 1)
	welcome, err := DecodeWelcome(written[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if welcome.Version != ProtocolVersion {
		t.Fatalf("WELCOME version = %d, want %d", welcome.Version, ProtocolVersion)
	}
	metrics := hub.Metrics()
	if metrics.HelloAdmissionAccepted != 1 || metrics.HelloAdmissionRefused != 0 {
		t.Fatalf("HELLO admission metrics = %+v", metrics)
	}
}

func TestHelloCommitRunsOnlyAfterWelcomeIsWritten(t *testing.T) {
	hub := newHub(testCfg())
	ticket := []byte("two-phase-ticket")
	var commits atomic.Int32
	hub.SetHelloCommitAuth(func(got []byte) (AdmissionIdentity, func() error, error) {
		if !bytes.Equal(got, ticket) {
			return AdmissionIdentity{}, nil, errors.New("unexpected ticket")
		}
		return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, func() error {
			commits.Add(1)
			return nil
		}, nil
	})
	// The gated writer cannot deliver WELCOME until the test opens the
	// gate, so the commit cannot have run early no matter how goroutines
	// interleave - asserting "not yet" against a blocked write is
	// deterministic where asserting it after AcceptConn returns is not.
	connection := newFakeConn(true)
	connection.inbound <- Frame{
		Opcode:  OpHello,
		Payload: EncodeHello(Hello{AdmissionToken: ticket}),
	}
	hub.AcceptConn(connection)
	wait.Consistently(t, 300*time.Millisecond, "no commit before WELCOME", func() bool {
		return commits.Load() == 0
	})
	connection.openGate()
	_ = waitWritten(t, connection, 1)
	waitUntil(t, "admission commit after WELCOME", func() bool { return commits.Load() == 1 })
	sessions := hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	t.Cleanup(func() { hub.closeSession(sessions[0], nil) })
}

func TestHelloTicketSurvivesConnectionDyingBeforeWelcome(t *testing.T) {
	hub := newHub(testCfg())
	ticket := []byte("fallback-ticket")
	var verifies, commits atomic.Int32
	hub.SetHelloCommitAuth(func(got []byte) (AdmissionIdentity, func() error, error) {
		if !bytes.Equal(got, ticket) {
			return AdmissionIdentity{}, nil, errors.New("unexpected ticket")
		}
		verifies.Add(1)
		return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, func() error {
			commits.Add(1)
			return nil
		}, nil
	})
	// The first attempt's writer never delivers WELCOME (gated) and the
	// connection dies first: the ticket must stay unconsumed.
	first := newFakeConn(true)
	first.inbound <- Frame{
		Opcode:  OpHello,
		Payload: EncodeHello(Hello{AdmissionToken: ticket}),
	}
	hub.AcceptConn(first)
	wait.Consistently(t, 300*time.Millisecond, "no commit before WELCOME", func() bool {
		return commits.Load() == 0
	})
	_ = first.Close("test: connection died before WELCOME")
	// The fallback presents the same ticket and is admitted; only its
	// WELCOME consumes.
	second := helloConn(nil, ticket)
	hub.AcceptConn(second)
	_ = waitWritten(t, second, 1)
	waitUntil(t, "fallback admission commit", func() bool { return commits.Load() == 1 })
	if got := verifies.Load(); got != 2 {
		t.Fatalf("verifies = %d, want 2 (verify never consumes)", got)
	}
	for _, session := range hub.Sessions() {
		t.Cleanup(func() { hub.closeSession(session, nil) })
	}
}

func TestHelloCommitReplayClosesTheSecondSession(t *testing.T) {
	hub := newHub(testCfg())
	ticket := []byte("raced-ticket")
	var commits atomic.Int32
	hub.SetHelloCommitAuth(func(got []byte) (AdmissionIdentity, func() error, error) {
		if !bytes.Equal(got, ticket) {
			return AdmissionIdentity{}, nil, errors.New("unexpected ticket")
		}
		return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, func() error {
			if commits.Add(1) > 1 {
				return errors.New("auth: transport admission token already used")
			}
			return nil
		}, nil
	})
	first := helloConn(nil, ticket)
	hub.AcceptConn(first)
	_ = waitWritten(t, first, 1)
	waitUntil(t, "first admission commit", func() bool { return commits.Load() == 1 })

	second := helloConn(nil, ticket)
	hub.AcceptConn(second)
	_ = waitWritten(t, second, 1)
	// The second WELCOME went out, but its consumption reports the
	// ticket already used: the session must not survive it.
	waitUntil(t, "replayed session closed", func() bool {
		return len(hub.Sessions()) == 1
	})
	for _, session := range hub.Sessions() {
		t.Cleanup(func() { hub.closeSession(session, nil) })
	}
}

func TestHelloAdmissionRefusalAllocatesNoSession(t *testing.T) {
	hub := newHub(testCfg())
	hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
		return AdmissionIdentity{}, errors.New("forged")
	})
	connection := helloConn(nil, []byte("forged-ticket"))
	hub.AcceptConn(connection)

	if got := len(hub.Sessions()); got != 0 {
		t.Fatalf("refused HELLO allocated %d session(s)", got)
	}
	written := connection.written()
	if len(written) != 1 || written[0].Opcode != OpBye ||
		!bytes.Equal(written[0].Payload, []byte{ByeReasonUnauthorized}) {
		t.Fatalf("refusal frames = %+v, want BYE(Unauthorized)", written)
	}
	metrics := hub.Metrics()
	if metrics.HelloAdmissionRefused != 1 || metrics.HelloAdmissionAccepted != 0 {
		t.Fatalf("HELLO admission metrics = %+v", metrics)
	}
}

func TestHelloWithoutAdmissionTicketIsRejectedAsMalformed(t *testing.T) {
	hub := newHub(testCfg())
	hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
		return AdmissionIdentity{}, errors.New("empty ticket")
	})
	connection := helloConn(nil, nil)
	hub.AcceptConn(connection)

	if got := len(hub.Sessions()); got != 0 {
		t.Fatalf("unauthenticated HELLO allocated %d session(s)", got)
	}
	written := connection.written()
	if len(written) != 1 || written[0].Opcode != OpBye ||
		!bytes.Equal(written[0].Payload, []byte{ByeReasonProtocolErr}) {
		t.Fatalf("malformed HELLO frames = %+v, want BYE(ProtocolErr)", written)
	}
}

func TestResumeIdentityMismatchCannotTakeSession(t *testing.T) {
	hub := newHub(testCfg())
	hub.SetHelloAuth(func(ticket []byte) (AdmissionIdentity, error) {
		switch string(ticket) {
		case "alice-ticket":
			return AdmissionIdentity{AccountID: "alice", ShardID: "global-official"}, nil
		case "bob-ticket":
			return AdmissionIdentity{AccountID: "bob", ShardID: "global-official"}, nil
		default:
			return AdmissionIdentity{}, errors.New("unknown ticket")
		}
	})
	first := helloConn(nil, []byte("alice-ticket"))
	hub.AcceptConn(first)
	sessions := hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("fresh sessions = %d, want 1", len(sessions))
	}
	session := sessions[0]
	t.Cleanup(func() { hub.closeSession(session, nil) })
	_ = first.Close("test disconnect")
	waitUntil(t, "authenticated session detach", func() bool {
		return hub.Metrics().DetachedSessions == 1
	})

	second := helloConn(
		session.resumeToken[:],
		[]byte("bob-ticket"),
	)
	hub.AcceptConn(second)

	written := second.written()
	if len(written) != 1 || written[0].Opcode != OpBye ||
		!bytes.Equal(written[0].Payload, []byte{ByeReasonUnauthorized}) {
		t.Fatalf("mismatch frames = %+v, want BYE(Unauthorized)", written)
	}
	if current, ok := hub.Session(session.ID); !ok || current != session {
		t.Fatal("identity mismatch removed or replaced the original session")
	}
	accountID, shardID, ok := session.AdmissionIdentity()
	if !ok || accountID != "alice" || shardID != "global-official" {
		t.Fatalf("original identity changed to (%q, %q, %v)", accountID, shardID, ok)
	}
}
