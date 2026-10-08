/*
===========================================================================

register.go - the party lane: rosters, invitations, member vitals

===========================================================================
*/

package party

import (
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
==================
ConsentArm

ConsentArm is one PEER invitation lane on the SHARED 0x3393 opcode
(guild today; mentor/TC type 9 when it ships). The party lane owns
the single hub registration, and a C->S reply routes to whichever
lane holds the answering character's outstanding invitation - the
reply cannot name its subsystem: the party composers 6FDE80/6FE050
accept with {01 01}, the guild accept is the byte-identical {01 01},
and the guild refuse's first byte is a RESULT code 2, never a type.
Implemented STRUCTURALLY (internal/game/social/guild's InviteRuntime) because the
import points party -> community -> guild and can never point back;
wiring.go performs the hookup.
==================
*/
type ConsentArm interface {
	// HasPendingInvite reports whether this lane holds an outstanding
	// invitation targeting the character (the router's ownership probe).
	HasPendingInvite(divisionID, name string) bool
	// ApplyConsent answers a routed reply: the two raw body bytes ride
	// verbatim (byte0 result/type, byte1 button/code) - the arm owns
	// their reading.
	ApplyConsent(s *transport.Session, divisionID string, actor *enterworld.Character, first, second uint8)
	// DropPendingInvite clears this lane's invitation for the character
	// when their session ends: a fresh client shows no prompt.
	DropPendingInvite(divisionID, name string) bool
}

/*
==================
MemberVitals

Runtime owns the party lane's registry and the collaborators it
resolves peers through: the shared deps pointer and the community
presence facade (the ONE cross-session lookup - this lane keeps no
session table of its own), plus the registered peer consent arms on
the shared 0x3393 opcode.
MemberVitals is the gameplay current and maximum for one character.
Wiring injects the keeper read. A nil hook keeps the closed-form
readers, which overstate a +HP member as full.
==================
*/
type MemberVitals func(divisionID string, character *enterworld.Character) (currentHP, maxHP, currentMP, maxMP int64)

// LivePose reads a character's live world position (the movement owner's
// interpolated spawn), which the native invite range gate measures.
type LivePose func(divisionID string, character *enterworld.Character) simulation.Spawn

const (
	// playerHitRange is CGObjChar_CheckHitRange's (4A8E10) range for a
	// target whose vtable+0x1C CGObjChar_IsPlayer holds.
	playerHitRange = 600
	// inviteWrongTarget and inviteTooFar are 4A8E10's results 3 (wrong
	// class or not same-plane adjacent sectors) and 4 (beyond the range).
	// CGObjPC_OnPartyFormRequest (5143B0) returns them in the request's
	// error ack; the client shows 0x200+code: 515 invalid target, 516 too far.
	inviteWrongTarget uint8 = 3
	inviteTooFar      uint8 = 4
)

/*
================
Runtime
Owns party membership and its session-scoped collaborators.
================
*/
type Runtime struct {
	deps         Dependencies
	presence     Presence
	registry     *Registry
	consentArms  []ConsentArm
	memberVitals MemberVitals
	livePose     LivePose
	updates      memberUpdateState
	masteries    bool
}

// NewRuntime builds the lane over a fresh in-memory registry. Construct
// it over the process-owned deps pointer shared by every lane.
/*
================
NewRuntime
================
*/
func NewRuntime(deps Dependencies, presence Presence) *Runtime {
	return &Runtime{deps: deps, presence: presence, registry: NewRegistry()}
}

// Registry exposes the party registry for tests.
/*
================
Registry
================
*/
func (r *Runtime) Registry() *Registry {
	return r.registry
}

/*
================
sessionByName
================
*/
func (r *Runtime) sessionByName(divisionID, characterName string) (*transport.Session, bool) {
	if r.presence == nil {
		return nil, false
	}
	return r.presence.SessionByName(divisionID, characterName)
}

/*
==================
UseMemberVitals

AddConsentArm registers one peer invitation lane on the shared 0x3393
routing (wiring.go calls it BEFORE any session enters the world; the
slice is never mutated after registration, so the handlers read it
without a lock).
UseMemberVitals installs the keeper HP/MP read used by roster rows.
==================
*/
func (r *Runtime) UseMemberVitals(fn MemberVitals) {
	if r != nil {
		r.memberVitals = fn
	}
}

/*
==================
UseLivePose

UseLivePose installs the live position read the invite range gate uses.
Without it every invite target is refused as out of range: a missing
position source must never admit a cross-map proposal.
==================
*/
func (r *Runtime) UseLivePose(fn LivePose) {
	if r != nil {
		r.livePose = fn
	}
}

/*
==================
inviteRange

CGObjChar_CheckHitRange (4A8E10) as CGObjPC_OnPartyFormRequest (5143B0)
calls it for the proposed member: same plane and adjacent sectors, then
the 3D distance within the player class range. 0 admits; otherwise the
native result code for the refusal ack.
==================
*/
func (r *Runtime) inviteRange(divisionID string, actor, target *enterworld.Character) uint8 {
	if r.livePose == nil {
		return inviteTooFar
	}
	from, to := r.livePose(divisionID, actor), r.livePose(divisionID, target)
	if !world.SamePlaneAdjacent(from.RegionID, to.RegionID) {
		return inviteWrongTarget
	}
	distance := monster.NativeActorDistance(
		monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z},
		monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z},
	)
	// 4A8E10 refuses when the range is below the distance.
	if playerHitRange < distance {
		return inviteTooFar
	}
	return 0
}

/*
================
AddConsentArm
================
*/
func (r *Runtime) AddConsentArm(arm ConsentArm) {
	if arm != nil {
		r.consentArms = append(r.consentArms, arm)
	}
}

/*
==================
Register

Register wires the five party C->S opcodes onto the hub. Each
registers exactly once and none is registered by any other lane (hub
registration is last-write-wins; 0x3393 is the SHARED invitation
multiplex - this lane owns the hub registration, and
handleInvitationConsent routes replies by PENDING-INVITE OWNERSHIP to
the registered ConsentArms, so the guild (and later mentor/TC) lane
hooks in through AddConsentArm rather than re-registering). Existing-
member refusals publish the verified category-2 errors below. Other
refusal/timeout producers still require native-contract recovery;
a mapped client error string alone does not prove a server predicate.
==================
*/
func (r *Runtime) Register(hub *transport.Hub) {
	hub.Handle(OpPartyInviteRequest, r.handleInvite)
	hub.Handle(OpPartyJoinInviteRequest, r.handleJoinInvite)
	hub.Handle(OpInvitationProposal, r.handleInvitationConsent)
	hub.Handle(OpPartyLeaveRequest, r.handleLeave)
	hub.Handle(OpPartyBanishRequest, r.handleBanish)
}

// sessionIdentity resolves the bound character or reports the silent
// discard (unbound sessions never reach party state).
/*
================
sessionIdentity
================
*/
func (r *Runtime) sessionIdentity(s *transport.Session, opcode uint16) (*enterworld.Character, string, bool) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		log.Debugf("party: 0x%04X from unbound session %d discarded", opcode, s.ID)
		return nil, "", false
	}
	character = characterSnapshot(r.deps, divisionID, character)
	if character == nil || character.DeletePending {
		log.Debugf("party: 0x%04X from unavailable character on session %d discarded", opcode, s.ID)
		return nil, "", false
	}
	return character, divisionID, true
}

/*
==================
findCharacterByGid

findCharacterByGid resolves an invite target's world gid back onto the
division character record: the 0x70D5/0x751A char-refs are the SAME
100000+ID band enterworld.ObjectIDForCharacter latches at enter-world
(there is no second id space to translate through).
==================
*/
func findCharacterByGid(deps Dependencies, divisionID string, gid uint32) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && enterworld.ObjectIDForCharacter(candidate) == gid {
			return characterSnapshot(deps, divisionID, candidate)
		}
	}
	return nil
}

/*
==================
findCharacterByName

findCharacterByName resolves a division character record by name,
case-insensitively (registry member names come from the store, but
the fold stays defensive like the community lane's).
==================
*/
func findCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return characterSnapshot(deps, divisionID, candidate)
		}
	}
	return nil
}

/*
==================
characterSnapshot

characterSnapshot copies mutable character state while the authority read
door is held. Party state is session-scoped, but its roster projections and
eligibility decisions still must not retain live store records.
==================
*/
func characterSnapshot(
	deps Dependencies,
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if deps == nil || character == nil {
		return nil
	}
	var snapshot *enterworld.Character
	deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	return snapshot
}

/*
==================
memberRowFor

memberRowFor composes one masked wire row from the live character
record: gid, name, the model resolve chain the local-player entry
uses, the persisted level (>=1 floor), the derived-vitals status
nibbles, and the settled spawn position. The packed world identity must
match enter-world admission; zero is a real instance, not the overworld.
==================
*/
func (r *Runtime) memberRowFor(divisionID string, character *enterworld.Character) MemberRow {
	row := MemberRow{
		MemberID: enterworld.ObjectIDForCharacter(character),
		Level:    1,
		War:      domain.CharacterWorldInstance(character),
	}
	if character == nil {
		return row
	}
	row.Name = character.Name
	if r.masteries {
		row.Masteries = true
		row.PrimaryMastery, row.SecondaryMastery = domain.TopMasteries(character.Masteries)
	}
	row.ModelRefID = r.deps.CharacterModelRef(character)
	if character.Level != nil && *character.Level >= 1 {
		level := *character.Level
		if level > 0xFF {
			level = 0xFF
		}
		row.Level = uint8(level)
	}
	var currentHP, maxHP, currentMP, maxMP int64
	if r.memberVitals != nil {
		currentHP, maxHP, currentMP, maxMP = r.memberVitals(divisionID, character)
	} else {
		maxHP = enterworld.DerivedMaxHP(character)
		maxMP = enterworld.DerivedMaxMP(character)
		currentHP = enterworld.CurrentHP(character)
		currentMP = enterworld.CurrentMP(character)
	}
	row.StatusNibbles = VitalStatusNibbles(currentHP, maxHP, currentMP, maxMP)
	world := enterworld.WorldStateForCharacter(character, enterworld.ResolveCharacterRaceKey(character))
	row.Region = uint16(world.Spawn.RegionID)
	row.PosX = int16(world.Spawn.X)
	row.PosY = int16(world.Spawn.Y)
	row.PosZ = int16(world.Spawn.Z)
	return row
}

/*
==================
rosterRows

rosterRows composes the 0x35D6 roster from a snapshot: one row per
member off the live character records, in roster order. A member
whose record vanished mid-flight degrades to the bare id+name row.
==================
*/
func (r *Runtime) rosterRows(divisionID string, snapshot Snapshot) []MemberRow {
	rows := make([]MemberRow, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		character := findCharacterByName(r.deps, divisionID, member.Name)
		if character == nil {
			rows = append(rows, MemberRow{MemberID: member.MemberID, Name: member.Name, Level: 1})
			continue
		}
		rows = append(rows, r.memberRowFor(divisionID, character))
	}
	return rows
}

/*
==================
sendPartySeed

sendPartySeed pushes the enter-a-party pair to ONE session: 0xB0D5
result=1 with the receiver's OWN member id (the only pinned setter of
stateBlock+0x18, which every later is-me split compares against - so
EVERY joiner receives it, not just the 0x70D5 requester; the ack also
clears the +0x4f9 pending latch, a no-op for a member who requested
nothing), then the 0x35D6 settings+roster bulk.
==================
*/
func sendPartySeed(s *transport.Session, myMemberID uint32, snapshot Snapshot, rows []MemberRow) {
	_ = s.Send(OpCreatePartyAck, EncodeCreatePartyAckB0D5(myMemberID))
	_ = s.Send(OpPartyInfo, EncodePartyInfo35D6(snapshot.LeaderID, snapshot.OptionBits, rows))
}

/*
==================
handleInvite

handleInvite applies one 0x70D5 create proposal: resolve the target by
world gid within the actor's division, refuse silently (self, unknown,
offline, either side already partied), then PROMPT the target with
0x3393 {u8 1, u32 inviterGid} (the sub_7644e0 party arm) and park the
proposal in the pending-invitation table. NOTHING commits here - the
party forms only when the target's 0x3393 accept consent arrives.
==================
*/
func (r *Runtime) handleInvite(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	request, err := DecodePartyInviteRequest(payload)
	if err != nil {
		log.Debugf("party: 0x%04X malformed from %s: %v", opcode, actor.Name, err)
		return
	}
	// The client only composes 0x70D5 with NO active party; an already
	// partied proposer is a desync or a forged frame - refuse before a
	// ghost prompt reaches the target.
	if _, partied := r.registry.PartyOf(divisionID, actor.Name); partied {
		log.Debugf("party: 0x70D5 (invite) refused for %s: already in a party", actor.Name)
		return
	}
	target, targetSession, refusal := r.resolveInviteTarget(divisionID, actor, request.TargetRef)
	if refusal != "" {
		log.Debugf("party: 0x70D5 (invite) refused for %s: %s", actor.Name, refusal)
		return
	}
	if code := r.inviteRange(divisionID, actor, target); code != 0 {
		log.Debugf("party: 0x70D5 (invite) refused for %s: %s out of range (%d)", actor.Name, target.Name, code)
		_ = s.Send(OpCreatePartyAck, []byte{2, code})
		return
	}
	if _, partied := r.registry.PartyOf(divisionID, target.Name); partied {
		log.Debugf("party: 0x70D5 (invite) refused for %s: target already in a party", actor.Name)
		_ = s.Send(OpCreatePartyAck, []byte{2, 0x18})
		return
	}
	if r.proposalPending(divisionID, target.Name) {
		log.Debugf("party: 0x70D5 (invite) refused for %s: %s already has a proposal waiting", actor.Name, target.Name)
		r.refuseBusyTarget(s, OpCreatePartyAck, targetSession)
		return
	}
	r.registry.SetPendingInvite(divisionID, target.Name, PendingInvite{
		Kind:        PendingInviteForm,
		InviterName: actor.Name,
		OptionBits:  request.OptionBits,
	})
	_ = targetSession.Send(OpInvitationProposal, EncodeInvitationPrompt3393(InvitationTypeParty, enterworld.ObjectIDForCharacter(actor), request.OptionBits))
	log.Debugf("party: %s proposed a party to %s (options 0x%02X) - prompt sent", actor.Name, target.Name, request.OptionBits)
}

/*
==================
handleJoinInvite

handleJoinInvite applies one 0x751A in-party proposal: the actor must
be partied and privileged (leader, or the party's join-anyone option
- the same gate the client dispatch sub_5b78e0 takes), the target
resolvable, online and partyless, the roster under its experience-mode
capacity. The target gets a 0x3393 type-3 prompt (formation uses type 2);
the join commits only on their accept consent.
==================
*/
func (r *Runtime) handleJoinInvite(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	targetRef, err := DecodePartyJoinInviteRequest(payload)
	if err != nil {
		log.Debugf("party: 0x%04X malformed from %s: %v", opcode, actor.Name, err)
		return
	}
	snapshot, inParty := r.registry.PartyOf(divisionID, actor.Name)
	if !inParty {
		log.Debugf("party: 0x751A (join-invite) refused for %s: not in a party", actor.Name)
		return
	}
	actorID := enterworld.ObjectIDForCharacter(actor)
	if snapshot.LeaderID != actorID && snapshot.OptionBits&PartyOptionJoinAnyone == 0 {
		log.Debugf("party: 0x751A (join-invite) refused for %s: not the leader and join-anyone is off", actor.Name)
		return
	}
	if len(snapshot.Members) >= partyCapacity(snapshot.OptionBits) {
		log.Debugf("party: 0x751A (join-invite) refused for %s: party is full", actor.Name)
		code := byte(0x14)
		if snapshot.OptionBits&PartyOptionExpShare != 0 {
			code = 0x13
		}
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, code})
		return
	}
	target, targetSession, refusal := r.resolveInviteTarget(divisionID, actor, targetRef)
	if refusal != "" {
		log.Debugf("party: 0x751A (join-invite) refused for %s: %s", actor.Name, refusal)
		return
	}
	// INFERENCE: the in-party proposal has the same target class and reach
	// as the form request; the client composes both from a selected player.
	if code := r.inviteRange(divisionID, actor, target); code != 0 {
		log.Debugf("party: 0x751A (join-invite) refused for %s: %s out of range (%d)", actor.Name, target.Name, code)
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, code})
		return
	}
	if targetParty, partied := r.registry.PartyOf(divisionID, target.Name); partied {
		log.Debugf("party: 0x751A (join-invite) refused for %s: target already in a party", actor.Name)
		// Research server 514063..51407C compares party IDs: same ->
		// 2C12, different -> 2C18. Client 75B100 reads a BYTE on B51A;
		// category 2 independently pins existing/other-party member text.
		code := byte(0x18)
		if targetParty.ObjectOrder == snapshot.ObjectOrder {
			code = 0x12
		}
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, code})
		return
	}
	if r.proposalPending(divisionID, target.Name) {
		log.Debugf("party: 0x751A (join-invite) refused for %s: %s already has a proposal waiting", actor.Name, target.Name)
		r.refuseBusyTarget(s, OpPartyJoinInviteAck, targetSession)
		return
	}
	r.registry.SetPendingInvite(divisionID, target.Name, PendingInvite{
		Kind:        PendingInviteJoin,
		InviterName: actor.Name,
	})
	_ = targetSession.Send(OpInvitationProposal, EncodeInvitationPrompt3393(InvitationTypePartyJoin, actorID, snapshot.OptionBits))
	log.Debugf("party: %s proposed joining their party to %s - prompt sent", actor.Name, target.Name)
}

/*
==================
handleInvitationConsent

handleInvitationConsent applies one C->S 0x3393 {u8, u8} reply. The
opcode is the SHARED invitation multiplex and the reply does NOT name
its subsystem: party accepts use {01 01}, the
guild accept (sub_6971b0 case 0xc @0x006975c3) is the byte-identical
{01 01}, and the guild refuse {02 16} (@0x00697616) carries a RESULT
code first, never a type - so the router resolves ownership by WHO
HOLDS the answering character's outstanding invitation. At most one
lane holds one at a time: no lane proposes while another's proposal
waits (proposalPending, the native one-transaction-per-player rule).
A reply no lane owns drops silently - never
invited, already answered, a duplicate frame, or a prompt that died
with its session.
==================
*/
func (r *Runtime) handleInvitationConsent(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	consent, err := DecodeInvitationConsent3393(payload)
	if err != nil {
		log.Debugf("party: 0x%04X malformed from %s: %v", opcode, actor.Name, err)
		return
	}
	accepts := consent.InviteType == 1 && consent.Button == 1
	refuses := consent.InviteType == 2 && (consent.Button == 0x0c || consent.Button == 0x17)
	if r.registry.HasPendingInviteFor(divisionID, actor.Name) && (accepts || refuses) {
		button := ConsentButtonRefuse
		if consent.InviteType == 1 {
			button = ConsentButtonAccept
		}
		r.applyPartyConsent(s, divisionID, actor, button)
		return
	}
	for _, arm := range r.consentArms {
		if arm.HasPendingInvite(divisionID, actor.Name) {
			arm.ApplyConsent(s, divisionID, actor, consent.InviteType, consent.Button)
			return
		}
	}
	log.Debugf("party: 0x3393 {%d %#x} from %s dropped: no lane holds an outstanding invitation", consent.InviteType, consent.Button, actor.Name)
}

/*
==================
proposalPending

A player holds one unanswered proposal of any kind: the transaction
manager keys them by the answering player and refuses a second
(TransactionMgr_InsertUnique 46F420). Replies stay routable because
at most one lane ever holds a pending for a character.
==================
*/
func (r *Runtime) proposalPending(divisionID, targetName string) bool {
	if r.registry.HasPendingInviteFor(divisionID, targetName) {
		return true
	}
	for _, arm := range r.consentArms {
		if arm.HasPendingInvite(divisionID, targetName) {
			return true
		}
	}
	return false
}

/*
==================
refuseBusyTarget

A refused submit fails the proposal with reason 2 (Transaction_Submit-
OrReject 4E7337): PartyProposal_NotifyFormationFailure / JoinFailure
(5BE9B0 / 5BEA20) answer the proposer on its request ack and the target
on its party ack, both {2, 2}. Client category 2 code 2 is the native
generic refusal notice.
==================
*/
func (r *Runtime) refuseBusyTarget(proposer *transport.Session, ack uint16, target *transport.Session) {
	_ = proposer.Send(ack, []byte{2, partyErrorBusy})
	_ = target.Send(OpPartyJoinAck, []byte{2, partyErrorBusy})
}

/*
==================
resolveInviteTarget

resolveInviteTarget shares the invite-target validation: the gid must
resolve to a division character other than the actor, not
delete-pending, and ONLINE (the 0x3393 prompt needs a live session to
land on - and the client paints joined members as live).
==================
*/
func (r *Runtime) resolveInviteTarget(divisionID string, actor *enterworld.Character, targetRef uint32) (*enterworld.Character, *transport.Session, string) {
	target := findCharacterByGid(r.deps, divisionID, targetRef)
	if target == nil {
		return nil, nil, "target gid not in the division"
	}
	if target.ID == actor.ID {
		return nil, nil, "cannot invite yourself"
	}
	if target.DeletePending {
		return nil, nil, "target is delete-pending"
	}
	targetSession, online := r.sessionByName(divisionID, target.Name)
	if !online {
		return nil, nil, "target is offline"
	}
	return target, targetSession, ""
}

/*
==================
handleLeave

handleLeave applies one 0x704F request - the empty-body frame whose
leave-vs-dissolve split the server decides by leadership (the
sub_6fdc30 fold's note): the leader leaving BREAKS the party (0x3E58
type-1 to every member including the initiator - full clear + the
BROKEN guide); a member leaving emits type-3 SECEDE to everyone (the
leaver's own is-me split full-clears their client), and a departure
that leaves fewer than two members breaks the remainder too.
==================
*/
func (r *Runtime) handleLeave(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	if err := DecodePartyLeaveRequest(payload); err != nil {
		log.Debugf("party: 0x%04X malformed from %s: %v", opcode, actor.Name, err)
		return
	}
	if !r.dropMember(divisionID, actor.Name, PartyLeaveReasonSecede, s) {
		log.Debugf("party: 0x704F (leave) refused for %s: not in a party", actor.Name)
	}
}

/*
==================
handleBanish

handleBanish applies one 0x7664 request: leader-only, target by the
roster member id (the record+0x3c value the pane's slot route sends).
The banished member gets 0x3E58 type-3 BOOTED (their is-me split
full-clears + the booted toast), every remaining member the same
removal, and a roster dropping below two breaks for the remainder.
==================
*/
func (r *Runtime) handleBanish(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	memberID, err := DecodePartyBanishRequest(payload)
	if err != nil {
		log.Debugf("party: 0x%04X malformed from %s: %v", opcode, actor.Name, err)
		return
	}
	outcome, refusal := r.registry.Banish(divisionID, actor.Name, memberID)
	if refusal != "" {
		log.Debugf("party: 0x7664 (banish) refused for %s: %s", actor.Name, refusal)
		return
	}
	leaveFrame := EncodePartyLeave3E58(outcome.Leaver.MemberID, PartyLeaveReasonBooted)
	if banishedSession, online := r.sessionByName(divisionID, outcome.Leaver.Name); online {
		_ = banishedSession.Send(OpPartyUpdate, leaveFrame)
	}
	r.notifyDeparture(divisionID, outcome, leaveFrame)
	log.Debugf("party: %s banished %s (member %d)", actor.Name, outcome.Leaver.Name, outcome.Leaver.MemberID)
}

/*
==================
WorldBound

WorldBound is the enter-world hook, called from the server's
OnWorldBound tail on the winner path: a character still carried by
the registry (their previous session was replaced without closing
first) is DROPPED like a logout - the fresh client holds no party
state, and reseeding it here would race the bootstrap frame order,
so the honest posture is the same departure a disconnect takes
(DECISION: a session boundary always drops the member; in-memory
party state does not survive the player's transport).

A resumed transport is not a session boundary: it re-enters the same
character on the same session (s.Rebound), the client keeps its party
across that entry as it does across a teleport, and the member stays.
Dropping here left the resumed client holding a party the server had
dissolved. A nil session (detached callers) is a new admission.
==================
*/
func (r *Runtime) WorldBound(s *transport.Session, divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	// A prompt the PREVIOUS client received died with it - the fresh
	// client shows no box, so a consent from the new entry must find
	// nothing outstanding.
	if r.registry.DropPendingInviteFor(divisionID, character.Name) {
		log.Debugf("party: %s re-entered the world; stale pending invitation dropped", character.Name)
	}
	if s != nil && s.Rebound() {
		return
	}
	if r.dropMember(divisionID, character.Name, PartyLeaveReasonLogout, nil) {
		log.Debugf("party: %s re-entered the world; stale party membership dropped", character.Name)
	}
}

/*
==================
SessionClosed

SessionClosed is the disconnect hook, called from the hub's
OnSessionClose: resolve the closing session's bound character (an
evicted loser had its name key cleared at bind time and resolves to
nothing), skip when ANOTHER session already holds the bind key (the
rebind winner is live - the character never went offline, the friend
lane's FriendSessionClosed guard; without it a loser whose close hook
outraces the bind-time name-key clear would drop the winner's live
membership and consume its pending invitation), then drop them with
the LOGOUT reason - the pinned type-3 reason-1 leg
(UIIT_MSG_PARTY_LOGOUT) is the client's own taxonomy for a member
leaving by disconnect, the evidence that a logout REMOVES the member
rather than parking them offline-in-party.
==================
*/
func (r *Runtime) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.sessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	// The prompt died with the target's transport: drop the outstanding
	// invitation so nothing stale is answerable later.
	if r.registry.DropPendingInviteFor(divisionID, character.Name) {
		log.Debugf("party: %s disconnected; pending invitation dropped", character.Name)
	}
	if r.dropMember(divisionID, character.Name, PartyLeaveReasonLogout, nil) {
		log.Debugf("party: %s disconnected; party membership dropped", character.Name)
	}
}

/*
==================
dropMember

dropMember applies one departure and fans the pinned frames out:
leader gone -> 0x3E58 type-1 BROKEN to everyone (the native
leader-leave dissolve); member gone -> type-3 {id, reason} to
everyone (is-me clears the leaver), plus type-1 to the remainder
when the roster fell below two. leaverSession carries the initiator's
own session for the 0x704F path (nil on the disconnect hooks - the
leaver is gone and receives nothing). Reports whether a departure
applied.
==================
*/
func (r *Runtime) dropMember(divisionID, name string, reason uint8, leaverSession *transport.Session) bool {
	outcome, refusal := r.registry.Leave(divisionID, name)
	if refusal != "" {
		return false
	}
	if outcome.WasLeader {
		broken := EncodePartyBroken3E58()
		if leaverSession != nil {
			_ = leaverSession.Send(OpPartyUpdate, broken)
		}
		for _, member := range outcome.Others {
			if peer, online := r.sessionByName(divisionID, member.Name); online {
				_ = peer.Send(OpPartyUpdate, broken)
			}
		}
		return true
	}
	leaveFrame := EncodePartyLeave3E58(outcome.Leaver.MemberID, reason)
	if leaverSession != nil {
		_ = leaverSession.Send(OpPartyUpdate, leaveFrame)
	}
	r.notifyDeparture(divisionID, outcome, leaveFrame)
	return true
}

/*
==================
notifyDeparture

notifyDeparture pushes one member's removal to every remaining
pre-departure member: the type-3 frame first (the row removal +
reason toast), then the type-1 BROKEN when the departure dissolved
the remainder.
==================
*/
func (r *Runtime) notifyDeparture(divisionID string, outcome LeaveOutcome, leaveFrame []byte) {
	broken := EncodePartyBroken3E58()
	for _, member := range outcome.Others {
		peer, online := r.sessionByName(divisionID, member.Name)
		if !online {
			continue
		}
		_ = peer.Send(OpPartyUpdate, leaveFrame)
		if outcome.Dissolved {
			_ = peer.Send(OpPartyUpdate, broken)
		}
	}
}
