/*
===========================================================================

audit.go - the operator trail of refused GM commands

A GM command from a character without GMPrivilege is refused silently on
the wire (gmcommand.go), which also meant it left no trace: a player running
a client-side "GM" tool showed up in no log. Each denial is now a warning
naming the division, character and session, rate-limited per character so
a flooding client cannot flood the log, and the table that does the limiting
is bounded so it cannot grow without limit either.

===========================================================================
*/
package gmcommand

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// privilegeAuditInterval is how often one character's denials are
	// reported; the count in between is carried into the next report.
	privilegeAuditInterval = time.Minute
	// privilegeAuditMaxEntries bounds the rate-limit table. When it fills,
	// entries older than one interval are dropped; if none are, the table
	// resets (a report may then repeat early, never go missing).
	privilegeAuditMaxEntries = 4096
)

type privilegeAuditEntry struct {
	lastReport time.Time
	suppressed int
}

// privilegeAudit is owned by one GM command hub handler.
type privilegeAudit struct {
	mu      sync.Mutex
	entries map[string]*privilegeAuditEntry
	report  func(fields log.Fields)
}

/*
================
newPrivilegeAudit
================
*/
func newPrivilegeAudit() *privilegeAudit {
	return &privilegeAudit{
		entries: make(map[string]*privilegeAuditEntry),
		report: func(fields log.Fields) {
			log.WithFields(fields).Warn("gmcommand: GM command from a character without GM privilege refused")
		},
	}
}

/*
================
deny

Records one refused command and reports it unless this character was
reported within the last interval; suppressed denials are counted into the
next report.
================
*/
func (audit *privilegeAudit) deny(divisionID, character string, session uint64, payloadBytes int, now time.Time) {
	key := divisionID + "\x00" + character
	audit.mu.Lock()
	entry := audit.entries[key]
	if entry != nil && now.Sub(entry.lastReport) < privilegeAuditInterval {
		entry.suppressed++
		audit.mu.Unlock()
		return
	}
	suppressed := 0
	if entry != nil {
		suppressed = entry.suppressed
	} else {
		audit.makeRoom(now)
		entry = &privilegeAuditEntry{}
		audit.entries[key] = entry
	}
	entry.lastReport = now
	entry.suppressed = 0
	audit.mu.Unlock()
	audit.report(log.Fields{
		"division":     divisionID,
		"character":    character,
		"session":      session,
		"payloadBytes": payloadBytes,
		"suppressed":   suppressed,
	})
}

/*
================
makeRoom

Keeps the table under privilegeAuditMaxEntries. Called with mu held.
================
*/
func (audit *privilegeAudit) makeRoom(now time.Time) {
	if len(audit.entries) < privilegeAuditMaxEntries {
		return
	}
	for key, entry := range audit.entries {
		if now.Sub(entry.lastReport) >= privilegeAuditInterval {
			delete(audit.entries, key)
		}
	}
	if len(audit.entries) >= privilegeAuditMaxEntries {
		audit.entries = make(map[string]*privilegeAuditEntry)
	}
}
