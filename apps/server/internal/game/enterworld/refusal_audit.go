/*
===========================================================================

refusal_audit.go - a refused door must leave its characters as it found them

A conditional door (Update, UpdateMany and their deferred forms) treats a
false callback result as a refusal: no dirty mark, no transaction. It does
not undo what the callback already changed on the live record, so a
callback that edits the character and then refuses leaves the edit in
memory for the next committed door to persist. Under tests every such
door is audited: the persisted form of each character is captured before
the callback and compared after a refusal, and a difference fails at the
door that made it. Production runs the callback untouched.

An allocation measurement (the synthetic-shard tick guard, #1943) may
suspend the audit for its measured window only: the audit encodes each
character twice per door, which would otherwise dominate what the
measurement counts. The audit still runs around that window.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync/atomic"
	// testing.Testing() alone gates the audit: production binaries report
	// false and run every callback untouched.
	"testing"
)

// refusalAuditSuspended is set only by SuspendRefusalAudit, from a test.
var refusalAuditSuspended atomic.Bool

/*
================
SuspendRefusalAudit

Turns the audit off until the returned restore runs. For allocation
measurements only: a test that suspends it must restore it before any
other check runs, and must not run in parallel with other tests.
================
*/
func SuspendRefusalAudit(tb testing.TB) (restore func()) {
	tb.Helper()
	refusalAuditSuspended.Store(true)
	return func() { refusalAuditSuspended.Store(false) }
}

/*
================
refusalAudited

update, checked under tests: a false result with any character's persisted
form changed panics with the door's label.
================
*/
func refusalAudited(label string, characters []*Character, update func() bool) func() bool {
	if update == nil || !testing.Testing() || refusalAuditSuspended.Load() {
		return update
	}
	return func() bool {
		before := persistedForms(characters)
		if update() {
			return true
		}
		after := persistedForms(characters)
		for i := range before {
			if !bytes.Equal(before[i], after[i]) {
				panic(fmt.Sprintf("door %q refused after changing character %d (mutate a copy and assign it only on success; "+
					"a change made outside the door, such as by another goroutine, also shows here): before %s after %s",
					label, i, before[i], after[i]))
			}
		}
		return false
	}
}

/*
================
persistedForms

Each character's JSON encoding, the form the store persists; nil for a
nil character.
================
*/
func persistedForms(characters []*Character) [][]byte {
	forms := make([][]byte, len(characters))
	for i, c := range characters {
		if c == nil {
			continue
		}
		encoded, err := json.Marshal(c)
		if err != nil {
			panic(fmt.Sprintf("refusal audit: character %d does not encode: %v", i, err))
		}
		forms[i] = encoded
	}
	return forms
}
