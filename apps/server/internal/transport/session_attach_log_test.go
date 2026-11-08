/*
===========================================================================

session_attach_log_test.go - the session attached line names its account

The attach line is the server half of the incident join (#2558): an
entry-rejection incident names the transport session, and this line
names the account that session was admitted for, so either side can
be searched from the other.

===========================================================================
*/
package transport

import (
	"testing"

	logtest "github.com/sirupsen/logrus/hooks/test"
)

/*
================
TestSessionAttachedLogNamesTheAdmittedAccount
================
*/
func TestSessionAttachedLogNamesTheAdmittedAccount(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	hub, _ := manualClockHub(t, testCfg())

	admitted, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	// Production order (hub.go): admission is recorded before attach.
	hub.admitAccountSession("tester", admitted)
	t.Cleanup(func() { hub.closeSession(admitted, nil) })
	if err := admitted.attach(newFakeConn(false), false, 0); err != nil {
		t.Fatal(err)
	}

	anonymous, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(anonymous, nil) })
	if err := anonymous.attach(newFakeConn(false), false, 0); err != nil {
		t.Fatal(err)
	}

	attached := make([]map[string]any, 0, 2)
	for _, entry := range hook.AllEntries() {
		if entry.Message == "transport: session attached" {
			attached = append(attached, entry.Data)
		}
	}
	if len(attached) != 2 {
		t.Fatalf("attached lines = %d, want 2", len(attached))
	}
	if attached[0]["session"] != admitted.ID || attached[0]["account"] != "tester" {
		t.Errorf("admitted line = %v, want session %d account tester", attached[0], admitted.ID)
	}
	if _, named := attached[1]["account"]; named {
		t.Errorf("unadmitted line names an account: %v", attached[1])
	}
}
