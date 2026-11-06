/*
===========================================================================

client_session_limit_test.go - one client address cannot hold the shard

The conduct cap (Config.MaxSessionsPerClient, port-only #2671) counts
live sessions by the host their connection reports, across accounts:
past it a fresh HELLO is refused with BYE ServerBusy and no session is
created, while other addresses, a freed slot and the uncapped default
all admit.
===========================================================================
*/
package transport

import (
	"errors"
	"os"
	"testing"
)

/*
================
clientHub

A hub with the client cap armed whose admission maps every ticket to
its own account, so only the address can refuse a session.
================
*/
func clientHub(t *testing.T, perClient int) *Hub {
	t.Helper()
	cfg := testCfg()
	cfg.MaxSessionsPerAccount = 100
	cfg.MaxSessionsPerClient = perClient
	hub := newHub(cfg)
	hub.SetHelloAuth(func(ticket []byte) (AdmissionIdentity, error) {
		if len(ticket) == 0 {
			return AdmissionIdentity{}, errors.New("empty ticket")
		}
		return AdmissionIdentity{AccountID: "acct-" + string(ticket), ShardID: "global-official"}, nil
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
helloConnAt

One fresh HELLO arriving from the given client address.
================
*/
func helloConnAt(address, ticket string) *fakeConn {
	connection := newFakeConn(false)
	connection.addr = address
	connection.inbound <- Frame{
		Opcode: OpHello,
		Payload: EncodeHello(Hello{
			AdmissionToken: []byte(ticket),
		}),
	}
	return connection
}

/*
================
admitAt

Runs one fresh HELLO from address; returns the session it created, or
nil when the hub refused it (the connection then holds a BYE).
================
*/
func admitAt(t *testing.T, hub *Hub, address, ticket string) (*Session, *fakeConn) {
	t.Helper()
	before := map[uint64]bool{}
	for _, session := range hub.Sessions() {
		before[session.ID] = true
	}
	connection := helloConnAt(address, ticket)
	hub.AcceptConn(connection)
	for _, session := range hub.Sessions() {
		if !before[session.ID] {
			return session, connection
		}
	}
	return nil, connection
}

func TestClientSessionLimitRefusesNewSessionsPastTheCap(t *testing.T) {
	hub := clientHub(t, 2)
	first, _ := admitAt(t, hub, "203.0.113.9:41000", "one")
	second, _ := admitAt(t, hub, "203.0.113.9:41001", "two")
	if first == nil || second == nil {
		t.Fatal("sessions within the client cap were refused")
	}
	refused, connection := admitAt(t, hub, "203.0.113.9:41002", "three")
	if refused != nil {
		t.Fatal("a third session from one client address was admitted past the cap")
	}
	written := connection.written()
	if len(written) != 1 || written[0].Opcode != OpBye ||
		len(written[0].Payload) != 1 || written[0].Payload[0] != ByeReasonServerBusy {
		t.Fatalf("refused connection frames = %v, want one BYE ServerBusy", written)
	}
	if got := hub.Metrics().ClientSessionRefusals; got != 1 {
		t.Fatalf("client session refusals = %d, want 1", got)
	}
	other, _ := admitAt(t, hub, "198.51.100.7:41000", "four")
	if other == nil {
		t.Fatal("a session from another client address was refused")
	}
	hub.closeSession(first, nil)
	freed, _ := admitAt(t, hub, "203.0.113.9:41003", "five")
	if freed == nil {
		t.Fatal("a closed session's slot stayed taken")
	}
}

func TestClientSessionLimitOffAdmitsEveryAddress(t *testing.T) {
	hub := clientHub(t, 0)
	for _, ticket := range []string{"one", "two", "three", "four"} {
		if session, _ := admitAt(t, hub, "203.0.113.9:41000", ticket); session == nil {
			t.Fatalf("uncapped hub refused session %q", ticket)
		}
	}
	if got := hub.Metrics().ClientSessionRefusals; got != 0 {
		t.Fatalf("client session refusals = %d, want 0", got)
	}
}

func TestClientSessionLimitCountsByHostNotPort(t *testing.T) {
	hub := clientHub(t, 1)
	if session, _ := admitAt(t, hub, "203.0.113.9:41000", "one"); session == nil {
		t.Fatal("the first session was refused")
	}
	if refused, _ := admitAt(t, hub, "203.0.113.9:51999", "two"); refused != nil {
		t.Fatal("a second port of the same client address was admitted")
	}
	if session, _ := admitAt(t, hub, "203.0.113.10:41000", "three"); session == nil {
		t.Fatal("a neighbouring address was refused")
	}
}

func TestMaxSessionsPerClientLimitReadsTheFlag(t *testing.T) {
	for _, row := range []struct {
		value string
		set   bool
		want  int
	}{
		{"", false, 0},
		{"", true, 0},
		{"3", true, 3},
		{" 12 ", true, 12},
		{"0", true, 0},
		{"-2", true, 0},
		{"many", true, 0},
	} {
		if row.set {
			t.Setenv(EnvMaxSessionsPerClient, row.value)
		} else {
			t.Setenv(EnvMaxSessionsPerClient, "")
			os.Unsetenv(EnvMaxSessionsPerClient)
		}
		if got := MaxSessionsPerClientLimit(); got != row.want {
			t.Fatalf("MaxSessionsPerClientLimit(%q set=%v) = %d, want %d", row.value, row.set, got, row.want)
		}
	}
}
