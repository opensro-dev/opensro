/*
===========================================================================

approval.go - proposing, approving and auditing a settings change

Port-only, not native. A staff member proposes a tunable's new value with
a reason; a different staff member approves or rejects it; the proposer
may withdraw it. An approved Live value is swapped into the running shard
at once (tunables.Apply); an approved Restart value is stored and waits
for the next boot. Every step, refusals included, is one append-only audit
entry written in the same transaction as the change it records.

Who acts comes from the X-OpenSRO-Staff header, which the staff console
sets from its signed-in identity (ops #67) and which this listener trusts
only behind the operator guard. A write without it is refused (412): no
change is ever attributed to nobody.

The rules here are storage-free; a Store keeps proposals, overrides and
the audit.

===========================================================================
*/
package settingsapi

import (
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/tunables"
)

// The staff identity headers the console sets after its sign-in.
const (
	HeaderStaff     = "X-OpenSRO-Staff"
	HeaderStaffName = "X-OpenSRO-Staff-Name"
)

// maxReasonLength bounds a proposal's or a rejection's reason.
const maxReasonLength = 500

// The audit's actions and results.
const (
	ActionPropose  = "propose"
	ActionApprove  = "approve"
	ActionReject   = "reject"
	ActionWithdraw = "withdraw"
	ResultApplied  = "applied"
	ResultQueued   = "queued"
	ResultRejected = "rejected"
	ResultPending  = "pending"
	ResultRefused  = "refused"
)

// Errors a Store returns; the handler maps them to HTTP statuses.
var (
	ErrNotFound = errors.New("no such proposal")
	ErrConflict = errors.New("a proposal is already pending for this setting")
)

/*
================
Staff

Who acts: the console's stable id and a display name.
================
*/
type Staff struct {
	ID   string
	Name string
}

/*
================
Proposal

One pending change. To is the raw value, as the environment would hold it.
================
*/
type Proposal struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	From   string `json:"from"`
	To     string `json:"to"`
	By     string `json:"by"`
	ByName string `json:"byName"`
	At     int64  `json:"at"`
	Reason string `json:"reason"`
}

/*
================
AuditEntry
================
*/
type AuditEntry struct {
	Seq    int64  `json:"seq"`
	At     int64  `json:"at"`
	Who    string `json:"who"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Key    string `json:"key"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

/*
================
Store

What the approval flow needs kept. Propose and Decide are transactions:
the proposal change, the override and the audit entry commit together or
not at all.
================
*/
type Store interface {
	Overrides() (map[string]string, error)
	Pending() ([]Proposal, error)
	// Propose records p and its audit entry; ErrConflict when one is
	// already pending for p.Key.
	Propose(p Proposal, entry AuditEntry) error
	// Decide removes the pending proposal id after check accepts it,
	// records the audit entry check returns and, when override is set,
	// stores the key's new value. When check refuses, nothing changes and
	// its error is returned with the proposal found, so the refusal can be
	// audited. ErrNotFound (and a zero Proposal) for an unknown id.
	Decide(id string, check func(Proposal) (entry AuditEntry, override bool, err error)) (Proposal, error)
	// Refuse records an audit entry for a write that changed nothing.
	Refuse(entry AuditEntry) error
	Audit(limit int, before int64) ([]AuditEntry, error)
}

/*
================
Service

The approval flow over a Store. Now is the clock (milliseconds); NewID
names a proposal.
================
*/
type Service struct {
	Store Store
	Now   func() int64
	NewID func() string
}

/*
================
Service.Propose

A proposal of key's new value. Refused (and audited) when the value fails
the registry's rules or the reason is empty or too long.
================
*/
func (s *Service) Propose(who Staff, key, to, reason string) (Proposal, error) {
	reason = strings.TrimSpace(reason)
	from := currentRaw(key)
	entry := AuditEntry{At: s.Now(), Who: who.ID, Name: who.Name, Action: ActionPropose, Key: key, From: from,
		To: to, Reason: reason}
	if err := checkReason(reason); err != nil {
		return Proposal{}, s.refuse(entry, err)
	}
	if _, err := tunables.Validate(key, to); err != nil {
		return Proposal{}, s.refuse(entry, err)
	}
	p := Proposal{ID: s.NewID(), Key: key, From: from, To: strings.TrimSpace(to), By: who.ID, ByName: who.Name,
		At: entry.At, Reason: reason}
	entry.Result = ResultPending
	if err := s.Store.Propose(p, entry); err != nil {
		if errors.Is(err, ErrConflict) {
			return Proposal{}, s.refuse(entry, err)
		}
		return Proposal{}, err
	}
	return p, nil
}

/*
================
Service.Approve

A different staff member approves id. A Live value is swapped in once the
store has committed; a Restart value waits for the next boot.
================
*/
func (s *Service) Approve(who Staff, id string) (string, error) {
	var result string
	p, err := s.decide(who, id, ActionApprove, func(p Proposal, entry *AuditEntry) (bool, error) {
		if who.ID == p.By {
			return false, errSelfApproval
		}
		setting, ok := tunables.Lookup(p.Key)
		if !ok {
			return false, fmt.Errorf("unknown setting %q", p.Key)
		}
		if _, err := tunables.Validate(p.Key, p.To); err != nil {
			return false, err
		}
		result = ResultQueued
		if setting.Class == tunables.Live {
			result = ResultApplied
		}
		entry.Result = result
		return true, nil
	})
	if err != nil {
		return "", err
	}
	if result == ResultApplied {
		if _, applyErr := tunables.Apply(p.Key, p.To); applyErr != nil {
			// Stored but not live: the next boot applies it. Not expected,
			// since the check validated the value.
			log.WithError(applyErr).WithField("key", p.Key).Warn("settings: approved value stored but not applied live")
			result = ResultQueued
		}
	}
	return result, nil
}

/*
================
Service.Reject

A different staff member rejects id with a reason.
================
*/
func (s *Service) Reject(who Staff, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if err := checkReason(reason); err != nil {
		return s.refuse(AuditEntry{At: s.Now(), Who: who.ID, Name: who.Name, Action: ActionReject, Reason: reason}, err)
	}
	_, err := s.decide(who, id, ActionReject, func(p Proposal, entry *AuditEntry) (bool, error) {
		entry.Reason, entry.Result = reason, ResultRejected
		if who.ID == p.By {
			return false, errSelfApproval
		}
		return false, nil
	})
	return err
}

/*
================
Service.Withdraw

The proposer takes id back; nobody else may.
================
*/
func (s *Service) Withdraw(who Staff, id string) error {
	_, err := s.decide(who, id, ActionWithdraw, func(p Proposal, entry *AuditEntry) (bool, error) {
		entry.Result = ResultRejected
		if who.ID != p.By {
			return false, errNotProposer
		}
		return false, nil
	})
	return err
}

/*
================
Service.decide

One decision on id: check fills the audit entry and says whether the
key's value is stored. A refusal by check is audited and returned; an
unknown id is ErrNotFound.
================
*/
func (s *Service) decide(who Staff, id, action string, check func(Proposal, *AuditEntry) (bool, error)) (Proposal, error) {
	p, err := s.Store.Decide(id, func(p Proposal) (AuditEntry, bool, error) {
		entry := AuditEntry{At: s.Now(), Who: who.ID, Name: who.Name, Action: action, Key: p.Key, From: p.From,
			To: p.To, Reason: p.Reason}
		override, err := check(p, &entry)
		return entry, override, err
	})
	if err != nil && !errors.Is(err, ErrNotFound) && p.ID != "" {
		return p, s.refuse(AuditEntry{At: s.Now(), Who: who.ID, Name: who.Name, Action: action, Key: p.Key,
			From: p.From, To: p.To, Reason: p.Reason}, err)
	}
	return p, err
}

// errSelfApproval and errNotProposer are the two identity refusals.
var (
	errSelfApproval = errors.New("the proposer cannot decide their own proposal")
	errNotProposer  = errors.New("only the proposer may withdraw a proposal")
)

/*
================
Service.refuse

Audits a refused write and returns its error.
================
*/
func (s *Service) refuse(entry AuditEntry, cause error) error {
	entry.Result, entry.Error = ResultRefused, cause.Error()
	if err := s.Store.Refuse(entry); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

/*
================
checkReason
================
*/
func checkReason(reason string) error {
	if reason == "" {
		return errors.New("a reason is required")
	}
	if len(reason) > maxReasonLength {
		return fmt.Errorf("a reason is at most %d bytes", maxReasonLength)
	}
	return nil
}

/*
================
currentRaw

The running value of key as an operator would write it: a factor that is
off reads "off".
================
*/
func currentRaw(key string) string {
	setting, ok := tunables.Lookup(key)
	if !ok {
		return ""
	}
	v := tunables.Current().Value(key)
	if setting.Kind == tunables.Factor && !v.Enabled {
		return "off"
	}
	return fmt.Sprint(v.Number)
}
