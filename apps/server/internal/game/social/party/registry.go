/*
===========================================================================

registry.go - session-scoped party membership, invitations and loot rotation.

===========================================================================
*/
package party

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

// Member is one party roster slot: the character's world gid (the SAME
// u32 the entered stream's gid latch and the invite target char-refs
// live in - enterworld.ObjectIDForCharacter; there is no second party
// member-id space) and the character name the presence facade resolves
// sessions by.
/*
================
Member
================
*/
type Member struct {
	MemberID uint32
	Name     string
}

// Snapshot is one party's state as a handler reads it: value copies
// taken under the registry lock so fan-out composes without holding it.
// Members ride in join order with the leader first.
/*
================
Snapshot
================
*/
type Snapshot struct {
	// ObjectOrder is the process-local object-address order used by native
	// reward grouping. It is neither a leader ID nor a wire/persistent party ID.
	ObjectOrder uint64
	LeaderID    uint32
	OptionBits  uint8
	Members     []Member
}

// partyState is the live registry record behind the snapshots.
/*
================
partyState
================
*/
type partyState struct {
	divisionID string
	leaderID   uint32
	optionBits uint8
	members    []Member
	lootOrder  []uint32
}

// PendingInviteKind splits the two proposal shapes an invitation prompt
// can carry: FORM (0x70D5 - a two-member party forms on accept) and JOIN
// (0x751A - the target joins the inviter's existing party).
type PendingInviteKind uint8

const (
	PendingInviteForm PendingInviteKind = 1
	PendingInviteJoin PendingInviteKind = 2
)

// PendingInvite is one outstanding invitation: stored when the 0x70D5 /
// 0x751A proposal sends the 0x3393 prompt, consumed by the target's
// consent. Everything is re-validated at consent time - the party world
// may have changed while the prompt was up.
/*
================
PendingInvite
================
*/
type PendingInvite struct {
	Kind        PendingInviteKind
	InviterName string
	OptionBits  uint8
	createdAtMs int64
	divisionID  string
	targetName  string
}

// Registry is the in-memory party store: session-scoped by design (the
// state recon's verdict - parties die with the process, honestly; the
// e2e reboot leg asserts EMPTY, never pretends persistence). One mutex
// guards the byKey map, the pendingByTarget invitation table, and every
// partyState behind them; handlers mutate under it, then compose and
// send from the returned snapshots with no lock held (no Hub call ever
// runs inside).
/*
================
Registry
================
*/
type Registry struct {
	mu              sync.Mutex
	byKey           map[string]*partyState
	pendingByTarget map[string]PendingInvite
}

// NewRegistry builds an empty registry.
/*
================
NewRegistry
================
*/
func NewRegistry() *Registry {
	return &Registry{
		byKey:           make(map[string]*partyState),
		pendingByTarget: make(map[string]PendingInvite),
	}
}

// SetPendingInvite records the outstanding invitation for a target. The
// handlers never call it while any proposal for the target waits
// (Runtime.proposalPending), so it never replaces a live one.
/*
================
SetPendingInvite
================
*/
func (r *Registry) SetPendingInvite(divisionID, targetName string, invite PendingInvite) {
	r.SetPendingInviteAt(divisionID, targetName, invite, time.Now().UnixMilli())
}

// SetPendingInviteAt shares the mission clock's millisecond time domain.
/*
================
SetPendingInviteAt
================
*/
func (r *Registry) SetPendingInviteAt(divisionID, targetName string, invite PendingInvite, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite.createdAtMs, invite.divisionID, invite.targetName = nowMs, divisionID, targetName
	r.pendingByTarget[memberKey(divisionID, targetName)] = invite
}

// ExpirePendingInvites retires only records still owned by this lane. Native
// 46F1E0 expires after, not at, 30 seconds. Replacement starts a fresh clock;
// accepted/dropped invitations are absent and cannot expire a second time.
/*
================
ExpirePendingInvites
================
*/
func (r *Registry) ExpirePendingInvites(nowMs int64) []PendingInvite {
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []PendingInvite
	for key, invite := range r.pendingByTarget {
		if nowMs > invite.createdAtMs && nowMs-invite.createdAtMs > 30000 {
			delete(r.pendingByTarget, key)
			expired = append(expired, invite)
		}
	}
	return expired
}

// HasPendingInviteFor reports whether an invitation targets the
// character WITHOUT consuming it - the shared-0x3393 consent router's
// ownership probe (TakePendingInvite stays the answering path).
/*
================
HasPendingInviteFor
================
*/
func (r *Registry) HasPendingInviteFor(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.pendingByTarget[memberKey(divisionID, name)]
	return ok
}

// TakePendingInvite consumes the target's outstanding invitation. A
// consent with no pending record (never invited, already answered, or a
// duplicate/stale frame) reports false and the caller drops silently.
/*
================
TakePendingInvite
================
*/
func (r *Registry) TakePendingInvite(divisionID, targetName string) (PendingInvite, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memberKey(divisionID, targetName)
	invite, ok := r.pendingByTarget[key]
	if ok {
		delete(r.pendingByTarget, key)
	}
	return invite, ok
}

// DropPendingInviteFor clears the invitation targeting a character. The
// session-boundary hooks call it: a fresh client shows no prompt, so a
// consent from the NEW session must never commit a prompt the OLD
// session received. Reports whether a pending invitation was dropped.
/*
================
DropPendingInviteFor
================
*/
func (r *Registry) DropPendingInviteFor(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memberKey(divisionID, name)
	if _, ok := r.pendingByTarget[key]; !ok {
		return false
	}
	delete(r.pendingByTarget, key)
	return true
}

// PendingInviteCount reports the number of outstanding invitations (the
// reboot-empty and leak assertions).
/*
================
PendingInviteCount
================
*/
func (r *Registry) PendingInviteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendingByTarget)
}

// memberKey is the registry's member identity: divisionID + ":" +
// lowercase(name) - the SAME shape as the hub's exclusive bind key and
// the presence facade's lookup, so a session and a character name
// resolve to the same party.
/*
================
memberKey
================
*/
func memberKey(divisionID, name string) string {
	return divisionID + ":" + strings.ToLower(name)
}

// snapshotLocked copies the party state; callers hold r.mu.
/*
================
snapshotLocked
================
*/
func snapshotLocked(p *partyState) Snapshot {
	members := make([]Member, len(p.members))
	copy(members, p.members)
	return Snapshot{ObjectOrder: uint64(reflect.ValueOf(p).Pointer()), LeaderID: p.leaderID, OptionBits: p.optionBits, Members: members}
}

// RewardSnapshots takes one consistent roster view, including parties whose
// leaders changed. Object identity belongs to partyState for its lifetime.
/*
================
RewardSnapshots
================
*/
func (r *Registry) RewardSnapshots(division string) []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[*partyState]bool)
	var out []Snapshot
	for _, p := range r.byKey {
		if p.divisionID == division && !seen[p] {
			seen[p] = true
			out = append(out, snapshotLocked(p))
		}
	}
	return out
}

// Count reports the number of live parties (the reboot-empty assertion).
// The byKey map holds one entry per MEMBER; distinct parties are the
// distinct states behind them.
/*
================
Count
================
*/
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	distinct := make(map[*partyState]struct{}, len(r.byKey))
	for _, p := range r.byKey {
		distinct[p] = struct{}{}
	}
	return len(distinct)
}

// PartyOf resolves the party a character belongs to.
/*
================
PartyOf
================
*/
func (r *Registry) PartyOf(divisionID, name string) (Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byKey[memberKey(divisionID, name)]
	if !ok {
		return Snapshot{}, false
	}
	return snapshotLocked(p), true
}

// Form creates a two-member party: the 0x70D5 path (the client only
// composes it with no active party, and the proposal's target must be
// partyless too). Returns the refusal reason when nothing formed.
/*
================
Form
================
*/
func (r *Registry) Form(divisionID string, leader, second Member, optionBits uint8) (Snapshot, string) {
	leaderKey := memberKey(divisionID, leader.Name)
	secondKey := memberKey(divisionID, second.Name)
	if leaderKey == secondKey {
		return Snapshot{}, "cannot form a party with yourself"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byKey[leaderKey]; exists {
		return Snapshot{}, "already in a party"
	}
	if _, exists := r.byKey[secondKey]; exists {
		return Snapshot{}, "target already in a party"
	}
	p := &partyState{
		divisionID: divisionID,
		leaderID:   leader.MemberID,
		optionBits: optionBits & PartyOptionMask,
		members:    []Member{leader, second},
	}
	r.byKey[leaderKey] = p
	r.byKey[secondKey] = p
	return snapshotLocked(p), ""
}

// Join appends a member to the actor's existing party: the 0x751A path.
// Re-validates everything under the lock (the handler's pre-checks ran
// without it). Returns the POST-join snapshot.
/*
================
Join
================
*/
func (r *Registry) Join(divisionID, actorName string, joiner Member) (Snapshot, string) {
	joinerKey := memberKey(divisionID, joiner.Name)
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byKey[memberKey(divisionID, actorName)]
	if !ok {
		return Snapshot{}, "not in a party"
	}
	if _, exists := r.byKey[joinerKey]; exists {
		return Snapshot{}, "target already in a party"
	}
	if len(p.members) >= partyCapacity(p.optionBits) {
		return Snapshot{}, "party is full"
	}
	p.members = append(p.members, joiner)
	r.byKey[joinerKey] = p
	return snapshotLocked(p), ""
}

// LeaveOutcome is one applied departure (leave, banish, or disconnect).
/*
================
LeaveOutcome
================
*/
type LeaveOutcome struct {
	// Leaver is the departed member.
	Leaver Member
	// WasLeader reports the native leave-vs-dissolve split: the leader
	// leaving dissolves the whole party.
	WasLeader bool
	// Dissolved reports the party is gone - either the leader left, or
	// the departure dropped the roster below the two-member minimum.
	Dissolved bool
	// Others is every OTHER pre-departure member, in roster order - the
	// fan-out audience.
	Others []Member
}

// Leave removes a character from their party: the 0x704F path and the
// disconnect hooks. The leader leaving DISSOLVES the party (the native
// split the client fold notes - 0x704F is empty and the server decides
// by leadership); a member leaving that would leave fewer than two
// members dissolves it too (a one-member party is not a party).
/*
================
Leave
================
*/
func (r *Registry) Leave(divisionID, name string) (LeaveOutcome, string) {
	key := memberKey(divisionID, name)
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byKey[key]
	if !ok {
		return LeaveOutcome{}, "not in a party"
	}
	leaver, others := splitMember(p.members, key, p.divisionID)
	outcome := LeaveOutcome{
		Leaver:    leaver,
		WasLeader: leaver.MemberID == p.leaderID,
		Others:    others,
	}
	if outcome.WasLeader || len(others) < 2 {
		outcome.Dissolved = true
		for _, member := range p.members {
			delete(r.byKey, memberKey(p.divisionID, member.Name))
		}
		return outcome, ""
	}
	p.members = others
	delete(r.byKey, key)
	return outcome, ""
}

// Banish removes the member with the given id from the actor's party:
// the 0x7664 path. Leader-only; the leader cannot banish themselves
// (the client's slot route never composes it, so a self-banish is a
// desync or a forged frame). Dissolves below the two-member minimum.
/*
================
Banish
================
*/
func (r *Registry) Banish(divisionID, actorName string, memberID uint32) (LeaveOutcome, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byKey[memberKey(divisionID, actorName)]
	if !ok {
		return LeaveOutcome{}, "not in a party"
	}
	actor, _ := splitMember(p.members, memberKey(divisionID, actorName), p.divisionID)
	if actor.MemberID != p.leaderID {
		return LeaveOutcome{}, "not the party leader"
	}
	if memberID == p.leaderID {
		return LeaveOutcome{}, "cannot banish the leader"
	}
	var banished *Member
	for i := range p.members {
		if p.members[i].MemberID == memberID {
			banished = &p.members[i]
			break
		}
	}
	if banished == nil {
		return LeaveOutcome{}, "member id not in the party"
	}
	banishedKey := memberKey(p.divisionID, banished.Name)
	leaver, others := splitMember(p.members, banishedKey, p.divisionID)
	outcome := LeaveOutcome{
		Leaver: leaver,
		Others: others,
	}
	if len(others) < 2 {
		outcome.Dissolved = true
		for _, member := range p.members {
			delete(r.byKey, memberKey(p.divisionID, member.Name))
		}
		return outcome, ""
	}
	p.members = others
	delete(r.byKey, banishedKey)
	return outcome, ""
}

// splitMember partitions the roster into the keyed member and everyone
// else, preserving roster order.
/*
================
splitMember
================
*/
func splitMember(members []Member, key, divisionID string) (Member, []Member) {
	var target Member
	others := make([]Member, 0, len(members))
	for _, member := range members {
		if memberKey(divisionID, member.Name) == key {
			target = member
			continue
		}
		others = append(others, member)
	}
	return target, others
}
