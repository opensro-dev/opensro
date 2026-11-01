/*
===========================================================================

instance_invite.go - bringing a party into an instanced world, by consent

A quest programme that wants its party inside an instance calls
InvitePartyToInstance: the layer opens (Setup fires from the open),
the leader transfers at once, and every eligible party member gets the
native instance invitation - the shared 0x3393 prompt, type 7, whose
answer the party lane's consent router hands to this lane's arm
(InstanceInviteConsent) by pending-invite ownership, like the guild,
mentor, exchange and resurrection arms beside it. An accept transfers
the member in; a decline, or the 30-second answer window lapsing,
leaves them behind, and a layer nobody entered and nobody is still
deciding on closes again instead of idling to its deadline.

Eligibility is the port's reading of the native shape: the initiator
leads the party, the party runs the shared modes (experience and item
share - the only parties the instance quests admit), the member stands
in the leader's world, is not already inside a programme, holds no
other lane's unanswered prompt (the one-transaction rule), has no
trade transport out, and passes the programme's own predicate. The
option-bit and transport gates are INFERENCE: no native admission
table for the invitation itself was read; the quest programme's own
rules (step 5) refine them.

===========================================================================
*/

package action

import (
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// instanceInvitePromptType is the 0x3393 prompt type the client reads
// as the quest instance invitation (client 0x7644E0's type-7 arm).
const instanceInvitePromptType uint8 = 7

// instanceInviteAnswerWindowMs is the answer window: the shared
// transaction manager's 30 s (the guild arm's pinned value), reused for
// this lane's proposals.
const instanceInviteAnswerWindowMs = 30_000

// instanceInviteShareOptions are the party option bits an instance
// party must run: experience share and item share (party options
// 0x1|0x2, duplicated here because the import points party -> action
// nowhere; the party registry owns the originals).
const instanceInviteShareOptions uint8 = 0x3

/*
================
instanceInvite

One outstanding invitation: the lease the member would enter, where,
who proposed it, and the world the party stood in when it was
proposed. The party and world parts are re-validated against those
anchors at consent time - either may have changed while the prompt
was up (the guild arm's posture).
================
*/
type instanceInvite struct {
	leaderName  string
	leaderGid   uint32
	originWorld instance.ID
	lease       instance.Lease
	entry       simulation.Spawn
	expiresMs   int64
}

/*
================
instanceInviteBook

The outstanding invitations, by target. Session-scoped runtime state:
it legitimately dies on process reboot (the party registry's verdict).
================
*/
type instanceInviteBook struct {
	mu       sync.Mutex
	byTarget map[string]instanceInvite
}

/*
================
instanceInviteKey

divisionID + ":" + lowercase(name) - the hub's bind-key shape, as the
guild arm keys its table.
================
*/
func instanceInviteKey(division, name string) string {
	return division + ":" + strings.ToLower(name)
}

/*
================
put
================
*/
func (b *instanceInviteBook) put(division, name string, invite instanceInvite) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byTarget == nil {
		b.byTarget = make(map[string]instanceInvite)
	}
	b.byTarget[instanceInviteKey(division, name)] = invite
}

/*
================
pending

Reports an invitation still inside its answer window.
================
*/
func (b *instanceInviteBook) pending(division, name string, nowMs int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	invite, ok := b.byTarget[instanceInviteKey(division, name)]
	return ok && nowMs <= invite.expiresMs
}

/*
================
take

Consumes the target's invitation. An expired one is consumed too and
reports false: the answer came too late to act on.
================
*/
func (b *instanceInviteBook) take(division, name string, nowMs int64) (instanceInvite, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := instanceInviteKey(division, name)
	invite, ok := b.byTarget[key]
	if ok {
		delete(b.byTarget, key)
	}
	return invite, ok && nowMs <= invite.expiresMs
}

/*
================
drop

Forgets the target's invitation (their session ended: a fresh client
shows no prompt). Reports whether one dropped.
================
*/
func (b *instanceInviteBook) drop(division, name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := instanceInviteKey(division, name)
	_, ok := b.byTarget[key]
	delete(b.byTarget, key)
	return ok
}

/*
================
expire

Drops every lapsed invitation and returns the distinct leases they
hung on, so the caller can close layers left empty.
================
*/
func (b *instanceInviteBook) expire(nowMs int64) []instance.Lease {
	b.mu.Lock()
	defer b.mu.Unlock()
	var leases []instance.Lease
	seen := make(map[instance.Lease]bool)
	for key, invite := range b.byTarget {
		if nowMs <= invite.expiresMs {
			continue
		}
		delete(b.byTarget, key)
		if !seen[invite.lease] {
			seen[invite.lease] = true
			leases = append(leases, invite.lease)
		}
	}
	return leases
}

/*
================
pendingForLease

Reports a still-open invitation into one layer.
================
*/
func (b *instanceInviteBook) pendingForLease(lease instance.Lease, nowMs int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, invite := range b.byTarget {
		if invite.lease == lease && nowMs <= invite.expiresMs {
			return true
		}
	}
	return false
}

/*
================
InstanceInviteRequest

One programme's demand to bring a party into an instance. Eligible is
the programme's own member predicate (nil admits every member the
generic gates pass); DeadlineMs is the layer's hard end, absolute on
the runtime clock (zero: only closing ends it).
================
*/
type InstanceInviteRequest struct {
	Division   string
	LeaderName string
	Definition instance.DefinitionID
	Entry      simulation.Spawn
	DeadlineMs int64
	Eligible   func(*enterworld.Character) bool
}

/*
================
InstanceInviteResult

What the initiation did. OpenStatus is the opening's verdict (a
pre-open refusal - no party, wrong leader, unshared party - reports
NotMember); LeaderStatus is the leader's own transfer. Prompted and
Skipped name the members by the prompt they did or did not get.
================
*/
type InstanceInviteResult struct {
	Lease        instance.Lease
	OpenStatus   instance.Status
	LeaderStatus instance.Status
	Prompted     []string
	Skipped      []string
}

/*
================
InstancePartyFacts

The party facts a quest programme's talk gates read, answered under
the division lock: whether the named character was found, stands in
a party, leads it, runs the shared options this lane requires, and
has a trade transport summoned. The invitation itself re-checks what
it needs when it acts; these facts only choose the refusal line.
================
*/
type InstancePartyFacts struct {
	Found       bool
	InParty     bool
	Leader      bool
	Shared      bool
	Transported bool
}

/*
================
InstancePartyFacts

Answers InstancePartyFacts for one character.
================
*/
func (rt *Runtime) InstancePartyFacts(division, name string) InstancePartyFacts {
	unlock := rt.lockDivision(division)
	defer unlock()
	character := rt.findCharacter(division, name)
	if character == nil {
		return InstancePartyFacts{}
	}
	facts := InstancePartyFacts{Found: true, Transported: rt.transportSummoned(character)}
	if party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(character)); party != nil {
		facts.InParty = true
		facts.Leader = party.LeaderID == enterworld.ObjectIDForCharacter(character)
		facts.Shared = party.Options&instanceInviteShareOptions == instanceInviteShareOptions
	}
	return facts
}

/*
================
instanceInvitePrompt

The S->C 0x3393 type-7 prompt: the inviter's gid, then the struct
The client's QuestInstanceInvite_Read (0x764380) parses - u32, u8, u32, u8, u8,
u32, u32, u16 member count, that many member gids. The client acts on
none of the struct fields, so they compose as zeros except the member
gid list it reads past; the port depends on no field semantics
(step 0).
================
*/
func instanceInvitePrompt(leaderGid uint32, memberGids []uint32) []byte {
	w := wire.NewWriter(26 + 4*len(memberGids)).
		U8(instanceInvitePromptType).U32(leaderGid).
		U32(0).U8(0).U32(0).U8(0).U8(0).U32(0).U32(0).
		U16(uint16(len(memberGids)))
	for _, gid := range memberGids {
		w.U32(gid)
	}
	return w.Payload()
}

/*
================
InvitePartyToInstance

Opens the layer, moves the leader in, and prompts the eligible
members. A party-less leader goes in alone (the programme decides
whether its quest wants a party at all - the mechanism does not). If
the leader's own transfer fails the layer closes again at once: an
instance whose initiator never arrived holds nothing.
================
*/
func (rt *Runtime) InvitePartyToInstance(req InstanceInviteRequest) InstanceInviteResult {
	result := InstanceInviteResult{OpenStatus: instance.NotMember, LeaderStatus: instance.NotMember}
	unlock := rt.lockDivision(req.Division)
	defer unlock()

	leader := rt.findCharacter(req.Division, req.LeaderName)
	if leader == nil {
		return result
	}
	leaderGid := enterworld.ObjectIDForCharacter(leader)
	var memberGids []uint32
	if party := rt.rewardPartyOf(req.Division, leaderGid); party != nil {
		if party.LeaderID != leaderGid || party.Options&instanceInviteShareOptions != instanceInviteShareOptions {
			return result
		}
		memberGids = party.Members
	}

	originWorld := instance.ID(domain.CharacterWorldInstance(leader))
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{
		Division: req.Division, Definition: req.Definition, DeadlineMs: req.DeadlineMs,
	})
	result.Lease, result.OpenStatus = lease, status
	if status != instance.Success {
		return result
	}
	result.LeaderStatus = rt.transferCharacterToInstance(req.Division, leader, lease, req.Entry)
	if result.LeaderStatus != instance.Success {
		rt.closeInstanceWorld(req.Division, lease)
		return result
	}

	nowMs := rt.Now().UnixMilli()
	prompt := instanceInvitePrompt(leaderGid, memberGids)
	for _, gid := range memberGids {
		if gid == leaderGid {
			continue
		}
		member := rt.findCharacterByGid(req.Division, gid)
		if member == nil {
			continue // offline: no name to prompt under, nothing to send
		}
		if !rt.instanceInviteEligible(req, member, originWorld, nowMs) {
			result.Skipped = append(result.Skipped, member.Name)
			continue
		}
		rt.instanceInvites.put(req.Division, member.Name, instanceInvite{
			leaderName:  leader.Name,
			leaderGid:   leaderGid,
			originWorld: originWorld,
			lease:       lease,
			entry:       req.Entry,
			expiresMs:   nowMs + instanceInviteAnswerWindowMs,
		})
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(req.Division, member.Name, []wire.Frame{{Opcode: opInvitationProposal, Payload: prompt}})
		}
		result.Prompted = append(result.Prompted, member.Name)
	}
	return result
}

/*
================
instanceInviteEligible

The generic member gates (the file banner lists them). The caller
holds the division lock.
================
*/
func (rt *Runtime) instanceInviteEligible(req InstanceInviteRequest, member *enterworld.Character, originWorld instance.ID, nowMs int64) bool {
	if member.DeletePending {
		return false
	}
	if member.World != nil && member.World.InstanceReturn != nil {
		return false // already inside a programme
	}
	if instance.ID(domain.CharacterWorldInstance(member)) != originWorld {
		return false // not standing with the leader
	}
	if rt.transportSummoned(member) {
		return false
	}
	if rt.instanceInvites.pending(req.Division, member.Name, nowMs) {
		return false
	}
	if rt.ProposalPending != nil && rt.ProposalPending(req.Division, member.Name) {
		return false // another lane's prompt is already up
	}
	if req.Eligible != nil && !req.Eligible(member) {
		return false
	}
	return true
}

/*
================
closeInstanceLeaseIfUnattended

Closes a layer nobody is inside and nobody still holds an invitation
into: it would otherwise idle, empty, to its deadline. Takes the
division lock itself.
================
*/
func (rt *Runtime) closeInstanceLeaseIfUnattended(division string, lease instance.Lease) {
	if division == "" || rt.instanceInvites.pendingForLease(lease, rt.Now().UnixMilli()) {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if len(rt.instanceResidentNames(division, lease)) > 0 {
		return
	}
	rt.closeInstanceWorld(division, lease)
}

/*
================
expireInstanceInvites

The tick's sweep: lapsed invitations fall, and any layer they were the
last reason to keep closes. Called from tickInstanceWorlds.
================
*/
func (rt *Runtime) expireInstanceInvites(nowMs int64) {
	for _, lease := range rt.instanceInvites.expire(nowMs) {
		rt.closeInstanceLeaseIfUnattended(rt.instanceLeaseDivision(lease), lease)
	}
}

/*
================
instanceLeaseDivision

The division a lease was opened on, from the tracked-worlds table
(the book itself is keyed by target, not by layer). An untracked
lease resolves to "" and has no live layer to close.
================
*/
func (rt *Runtime) instanceLeaseDivision(lease instance.Lease) string {
	division := ""
	rt.instanceWorlds.Range(func(key, _ any) bool {
		if key.(instanceWorldKey).id == lease.ID {
			division = key.(instanceWorldKey).division
			return false
		}
		return true
	})
	return division
}

/*
================
DropInstanceInvite

Forgets a character's outstanding instance invitation (their session
ended or they re-entered the world: a fresh client shows no prompt).
Wiring calls it from the session hooks, like the other lanes' drops.
================
*/
func (rt *Runtime) DropInstanceInvite(division, name string) bool {
	return rt.instanceInvites.drop(division, name)
}

/*
===============================================================================

THE CONSENT ARM

===============================================================================
*/

/*
================
InstanceInviteConsent

The instance lane on the shared 0x3393 answer (party.ConsentArm,
implemented structurally): the party router hands it the answer of any
player it holds an invitation for.
================
*/
type InstanceInviteConsent struct {
	rt *Runtime
}

/*
================
InstanceInviteConsent

Returns the lane the composition root registers.
================
*/
func (rt *Runtime) InstanceInviteConsent() *InstanceInviteConsent {
	return &InstanceInviteConsent{rt: rt}
}

/*
================
HasPendingInvite

Reports an invitation still inside its answer window.
================
*/
func (c *InstanceInviteConsent) HasPendingInvite(divisionID, name string) bool {
	return c.rt.instanceInvites.pending(divisionID, name, c.rt.Now().UnixMilli())
}

/*
================
DropPendingInvite

Forgets the invitation for a character.
================
*/
func (c *InstanceInviteConsent) DropPendingInvite(divisionID, name string) bool {
	return c.rt.instanceInvites.drop(divisionID, name)
}

/*
================
ApplyConsent

The answer {1, 1} accepts; anything else declines (the client sends
{1, button} for both box buttons). The answered event fires either
way (the invitation-answered event); an accept then
re-checks the party and world parts of eligibility (instanceAcceptEligible)
and transfers the member in under the division lock, re-validated
live - a member who no longer fits simply does not move, and the
event stream says so by the missing Enter. Delivery: the accept
path's lane release delivers the queued answered/enter/activated
events in order; a decline takes no lane, so it delivers its
answered event itself - this entry point holds no lock either way
(instance_events.go).
================
*/
func (c *InstanceInviteConsent) ApplyConsent(_ *transport.Session, divisionID string, actor *enterworld.Character, first, second uint8) {
	rt := c.rt
	if actor == nil {
		return
	}
	invite, ok := rt.instanceInvites.take(divisionID, actor.Name, rt.Now().UnixMilli())
	if !ok {
		return
	}
	accepted := first == 1 && second == 1
	rt.instanceEvent(InstanceEventInvitationAnswered, divisionID, invite.lease.ID.Definition(), invite.lease.ID, actor, accepted)
	if !accepted {
		rt.closeInstanceLeaseIfUnattended(divisionID, invite.lease)
		// No lane is taken on this path, so no release will deliver
		// the queued answer; deliver it here, lock-free.
		rt.flushInstanceEvents()
		return
	}
	unlock := rt.lockDivision(divisionID)
	if member := rt.findCharacter(divisionID, actor.Name); member != nil && rt.instanceAcceptEligible(divisionID, member, invite) {
		rt.transferCharacterToInstance(divisionID, member, invite.lease, invite.entry)
	}
	unlock()
}

/*
================
instanceAcceptEligible

The party and world parts of the invitation-time gates, re-run at
consent (the prompt has been up for as long as 30 s): the member
still stands in the world the party was invited from, has not
entered another programme meanwhile, and still belongs to the
inviting leader's party. The caller holds the division lock. The
remaining invitation-time gates (delete-pending, trade transport,
competing proposals, the programme predicate) are not re-run - the
issue's scope is the two parts the answer window can silently
invalidate - and the transfer's own gates still run inside
transferCharacterToInstance.
================
*/
func (rt *Runtime) instanceAcceptEligible(divisionID string, member *enterworld.Character, invite instanceInvite) bool {
	if member.World != nil && member.World.InstanceReturn != nil {
		return false // inside another programme now
	}
	if instance.ID(domain.CharacterWorldInstance(member)) != invite.originWorld {
		return false // no longer standing in the inviting world
	}
	party := rt.rewardPartyOf(divisionID, enterworld.ObjectIDForCharacter(member))
	return party != nil && party.LeaderID == invite.leaderGid
}
