/*
===========================================================================

refusal_audit_test.go - the refused-door audit catches a leaked change

===========================================================================
*/
package enterworld

import (
	"strings"
	"testing"
)

/*
================
TestRefusedDoorThatChangedItsCharacterPanics

A callback that changes the live character and then refuses is caught at
the door; one that refuses untouched, or commits, passes.
================
*/
func TestRefusedDoorThatChangedItsCharacterPanics(t *testing.T) {
	d := &Deps{}
	gold := int64(10)
	c := &Character{Name: "audited", Gold: &gold}
	if d.Update(c, "clean-refusal", func() bool { return false }) {
		t.Fatal("a refusal reported a commit")
	}
	if !d.Update(c, "commit", func() bool { *c.Gold = 20; return true }) || *c.Gold != 20 {
		t.Fatal("a commit was lost")
	}
	defer func() {
		message, _ := recover().(string)
		if !strings.Contains(message, `door "leaky"`) {
			t.Fatalf("leaked change not caught: %q", message)
		}
	}()
	d.UpdateMany([]*Character{c}, "leaky", func() bool {
		*c.Gold = 30
		return false
	})
	t.Fatal("unreachable: the audit did not panic")
}

/*
================
TestSuspendRefusalAuditOnlyWhileSuspended

The tick guard suspends the audit for its measured window: while
suspended, a door that changes a character and then refuses passes
unchecked; after restore the same door fails the audit again.
================
*/
func TestSuspendRefusalAuditOnlyWhileSuspended(t *testing.T) {
	gold := int64(10)
	c := &Character{Name: "audited", Gold: &gold}
	dirtyRefusal := func() bool { *c.Gold++; return false }
	restore := SuspendRefusalAudit(t)
	func() {
		defer func() {
			if recover() != nil {
				t.Error("a suspended audit still checked the door")
			}
		}()
		refusalAudited("suspended", []*Character{c}, dirtyRefusal)()
	}()
	restore()
	defer func() {
		if recover() == nil {
			t.Fatal("the audit stayed off after restore")
		}
	}()
	refusalAudited("restored", []*Character{c}, dirtyRefusal)()
}
