/*
===========================================================================

password_failures.go - the per-client, per-account failure budget

Only failed credential checks consume it; success or expiry resets it.
Accounts are keyed by their folded id, so case changes share one budget.

===========================================================================
*/
package agentserver

import (
	"sync"
	"time"

	"opensro.online/server/internal/domain"
)

// Local gateway policy, not a claim about Joymax's server implementation.
// Unlike the IP request limiter, only failed credential checks consume this
// account/client budget. Successful authentication or expiry resets it.
const passwordFailureLimit = 5
const passwordFailureWindow = time.Minute

type passwordFailureKey struct{ client, account string }
type passwordFailureState struct {
	count   uint16
	expires time.Time
}
type passwordFailures struct {
	mu      sync.Mutex
	entries map[passwordFailureKey]passwordFailureState
}

/*
================
update
================
*/
func (failures *passwordFailures) update(client, account string, now time.Time, failed, success bool) (uint32, bool) {
	failures.mu.Lock()
	defer failures.mu.Unlock()
	// Keyed by the folded id: "Bob" and "bob" are one login, so changing case
	// must not open a fresh failure budget.
	key := passwordFailureKey{loginClientKey(client), domain.FoldAccountID(account)}
	if success {
		delete(failures.entries, key)
		return 0, true
	}
	state := failures.entries[key]
	if !now.Before(state.expires) {
		state = passwordFailureState{}
		delete(failures.entries, key)
	}
	if failed {
		if state.count == 0 {
			if failures.entries == nil {
				failures.entries = make(map[passwordFailureKey]passwordFailureState)
			}
			if len(failures.entries) >= maxLoginClientBuckets {
				for other, entry := range failures.entries {
					if !now.Before(entry.expires) {
						delete(failures.entries, other)
					}
				}
				if len(failures.entries) >= maxLoginClientBuckets {
					return 0, false
				}
			}
			state.expires = now.Add(passwordFailureWindow)
		}
		if state.count < passwordFailureLimit {
			state.count++
		}
		failures.entries[key] = state
	}
	return uint32(passwordFailureLimit)<<16 | uint32(state.count), true
}
