/*
===========================================================================

consent.go - resolve party proposals and report every terminal outcome.

Party formation and joining share one reply owner. A consumed invitation
must either publish membership or notify the surviving participants.

===========================================================================
*/
package party

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

const (
	partyFailureUnknown        byte = 0x02
	partyFailureCreatorMissing byte = 0x0e
	partyFailureTimeout        byte = 0x10
	partyFailureFormRefused    byte = 0x0c
	partyFailureJoinRefused    byte = 0x17
	// partyFailureJobMismatch is the reference shard manager's 0x2C23 for
	// two players whose job classes may not party; v1.150 shows
	// UIIT_MSG_PARTY_USER_MISSMATCH.
	partyFailureJobMismatch byte = 0x23
)

/*
================
partyConsent

Resolved participants for a single consumed proposal.
================
*/
type partyConsent struct {
	invite         PendingInvite
	targetSession  *transport.Session
	inviterSession *transport.Session
	inviter        *enterworld.Character
	target         *enterworld.Character
}

/*
================
notifyInvitationFailure

The reference server's formation and join failures fan out to both surviving peers; B452 is the invitee carrier.
The pending record retains the formation/join distinction after consumption.
================
*/
func (r *Runtime) notifyInvitationFailure(invite PendingInvite, code byte) {
	ack := OpCreatePartyAck
	if invite.Kind == PendingInviteJoin {
		ack = OpPartyJoinInviteAck
	}
	if session, online := r.sessionByName(invite.divisionID, invite.InviterName); online {
		_ = session.Send(ack, []byte{2, code})
	}
	if session, online := r.sessionByName(invite.divisionID, invite.targetName); online {
		_ = session.Send(OpPartyJoinAck, []byte{2, code})
	}
}

/*
================
applyPartyConsent

Consume once, revalidate live participants, then commit or report failure.
An already retired proposal remains a silent duplicate on the native wire.
================
*/
func (r *Runtime) applyPartyConsent(session *transport.Session, divisionID string, actor *enterworld.Character, button uint8) {
	invite, outstanding := r.registry.TakePendingInvite(divisionID, actor.Name)
	if !outstanding {
		return
	}
	if button != ConsentButtonAccept {
		code := partyFailureFormRefused
		if invite.Kind == PendingInviteJoin {
			code = partyFailureJoinRefused
		}
		r.notifyInvitationFailure(invite, code)
		return
	}
	inviter := findCharacterByName(r.deps, divisionID, invite.InviterName)
	inviterSession, online := r.sessionByName(divisionID, invite.InviterName)
	if inviter == nil || (!online && !inviter.RuntimeOnly) {
		// Natively 2C0E is reported when the proposer has disappeared.
		r.notifyInvitationFailure(invite, partyFailureCreatorMissing)
		return
	}
	consent := partyConsent{invite: invite, targetSession: session, inviterSession: inviterSession, inviter: inviter, target: actor}
	code := partyFailureUnknown
	switch invite.Kind {
	case PendingInviteForm:
		code = r.commitFormConsent(consent)
	case PendingInviteJoin:
		code = r.commitJoinConsent(consent)
	}
	if code != 0 {
		r.notifyInvitationFailure(invite, code)
		return
	}
	// A committed join changed the party for everyone in it now.
	if r.PartyChanged != nil {
		if snapshot, ok := r.registry.PartyOf(divisionID, actor.Name); ok {
			for _, member := range snapshot.Members {
				r.PartyChanged(divisionID, member.Name)
			}
		}
	}
}

/*
==================
commitFormConsent

commitFormConsent forms the two-member party an accepted 0x70D5
proposal described: the registry re-validates both sides partyless
under its lock, then both sessions get the 0xB0D5 + 0x35D6 seed.
==================
*/
func (r *Runtime) commitFormConsent(consent partyConsent) byte {
	targetSession, inviterSession := consent.targetSession, consent.inviterSession
	divisionID, inviter, target, optionBits := consent.invite.divisionID, consent.inviter, consent.target, consent.invite.OptionBits
	// The reference shard manager refuses the pair after its own partyless checks; the registry
	// repeats those under its lock when it forms the party.
	if !enterworld.JobsMayParty(enterworld.PartyJobClass(inviter), enterworld.PartyJobClass(target)) {
		return partyFailureJobMismatch
	}
	leader := Member{MemberID: enterworld.ObjectIDForCharacter(inviter), Name: inviter.Name}
	second := Member{MemberID: enterworld.ObjectIDForCharacter(target), Name: target.Name}
	snapshot, refusal := r.registry.Form(divisionID, leader, second, optionBits)
	if refusal != "" {
		// Inference: local registry rejection replaces a failed coordinator job;
		// use its generic failure, never silent success.
		return partyFailureUnknown
	}
	rows := r.rosterRows(divisionID, snapshot)
	if inviterSession != nil {
		sendPartySeed(inviterSession, leader.MemberID, snapshot, rows)
	}
	if targetSession != nil {
		sendPartySeed(targetSession, second.MemberID, snapshot, rows)
	}
	return 0
}

/*
==================
commitJoinConsent

commitJoinConsent joins the accepting target into the inviter's party:
the privilege gate re-runs against the LIVE snapshot (the party may
have re-formed or delegated while the prompt was up), the registry
re-validates membership/cap under its lock, then the sitting members
get the 0x3E58 type-2 JOIN row and the joiner the 0xB0D5 + 0x35D6
seed (which also clears the inviter's pending latch, the client's
CPSMission_OnPartyJobMemberJoinLogout0x3E58 leg).
==================
*/
func (r *Runtime) commitJoinConsent(consent partyConsent) byte {
	targetSession, divisionID := consent.targetSession, consent.invite.divisionID
	inviter, target := consent.inviter, consent.target
	snapshot, inParty := r.registry.PartyOf(divisionID, inviter.Name)
	if !inParty {
		return partyFailureUnknown
	}
	inviterID := enterworld.ObjectIDForCharacter(inviter)
	if snapshot.LeaderID != inviterID && snapshot.OptionBits&PartyOptionJoinAnyone == 0 {
		return partyFailureUnknown
	}
	// The reference shard manager compares the joiner with the party's job class. Members cannot
	// change suits inside a party (0xA0), so the leader's class is the
	// party's.
	if !enterworld.JobsMayParty(r.partyJobClass(divisionID, snapshot, inviter), enterworld.PartyJobClass(target)) {
		return partyFailureJobMismatch
	}
	joiner := Member{MemberID: enterworld.ObjectIDForCharacter(target), Name: target.Name}
	joined, refusal := r.registry.Join(divisionID, inviter.Name, joiner)
	if refusal != "" {
		// Inference: local registry rejection replaces a failed coordinator job;
		// use its generic failure, never silent success.
		return partyFailureUnknown
	}
	joinRow := r.memberRowFor(divisionID, target)
	for _, member := range joined.Members {
		if member.MemberID == joiner.MemberID {
			continue
		}
		if peer, online := r.sessionByName(divisionID, member.Name); online {
			_ = peer.Send(OpPartyUpdate, EncodePartyJoin3E58(joinRow))
		}
	}
	if targetSession != nil {
		sendPartySeed(targetSession, joiner.MemberID, joined, r.rosterRows(divisionID, joined))
	}
	return 0
}

/*
================
partyJobClass

The party's job class: its leader's. A leader that cannot be resolved
falls back to the inviting member, who shares it.
================
*/
func (r *Runtime) partyJobClass(divisionID string, snapshot Snapshot, inviter *enterworld.Character) uint8 {
	for _, member := range snapshot.Members {
		if member.MemberID != snapshot.LeaderID {
			continue
		}
		if leader := findCharacterByName(r.deps, divisionID, member.Name); leader != nil {
			return enterworld.PartyJobClass(leader)
		}
	}
	return enterworld.PartyJobClass(inviter)
}

func (r *Runtime) acceptRuntimeActorFormInvite(s *transport.Session, divisionID string, actor, target *enterworld.Character, optionBits uint8) {
	if r.CanPartyWithRuntimeActor == nil || !r.CanPartyWithRuntimeActor(divisionID, actor.Name, target.Name) {
		log.Debugf("party: 0x70D5 (invite) refused for %s: not contracted with %s", actor.Name, target.Name)
		_ = s.Send(OpCreatePartyAck, []byte{2, partyFailureFormRefused})
		return
	}
	if !enterworld.JobsMayParty(enterworld.PartyJobClass(actor), enterworld.PartyJobClass(target)) {
		_ = s.Send(OpCreatePartyAck, []byte{2, partyFailureJobMismatch})
		return
	}
	code := r.commitFormConsent(partyConsent{
		invite: PendingInvite{
			divisionID:  divisionID,
			Kind:        PendingInviteForm,
			InviterName: actor.Name,
			OptionBits:  optionBits,
		},
		inviterSession: s,
		inviter:        actor,
		target:         target,
	})
	if code != 0 {
		_ = s.Send(OpCreatePartyAck, []byte{2, code})
		return
	}
	if r.PartyChanged != nil {
		if snapshot, ok := r.registry.PartyOf(divisionID, actor.Name); ok {
			for _, member := range snapshot.Members {
				r.PartyChanged(divisionID, member.Name)
			}
		}
	}
}

func (r *Runtime) acceptRuntimeActorJoinInvite(s *transport.Session, divisionID string, snapshot Snapshot, actor, target *enterworld.Character) {
	if r.CanPartyWithRuntimeActor == nil || !r.CanPartyWithRuntimeActor(divisionID, actor.Name, target.Name) {
		log.Debugf("party: 0x751A (join-invite) refused for %s: not contracted with %s", actor.Name, target.Name)
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, partyFailureJoinRefused})
		return
	}
	if !enterworld.JobsMayParty(r.partyJobClass(divisionID, snapshot, actor), enterworld.PartyJobClass(target)) {
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, partyFailureJobMismatch})
		return
	}
	code := r.commitJoinConsent(partyConsent{
		invite: PendingInvite{
			divisionID:  divisionID,
			Kind:        PendingInviteJoin,
			InviterName: actor.Name,
			OptionBits:  snapshot.OptionBits,
		},
		inviterSession: s,
		inviter:        actor,
		target:         target,
	})
	if code != 0 {
		_ = s.Send(OpPartyJoinInviteAck, []byte{2, code})
		return
	}
	if r.PartyChanged != nil {
		if joinedSnapshot, ok := r.registry.PartyOf(divisionID, actor.Name); ok {
			for _, member := range joinedSnapshot.Members {
				r.PartyChanged(divisionID, member.Name)
			}
		}
	}
}

/*
==================
InvitePlayerFromRuntimeActor

Has a hired/contracted runtime actor propose or join a party with playerName.
==================
*/
func (r *Runtime) InvitePlayerFromRuntimeActor(divisionID, actorName, playerName string) bool {
	if r == nil {
		return false
	}
	actor := findCharacterByName(r.deps, divisionID, actorName)
	if actor == nil || !actor.RuntimeOnly {
		return false
	}
	target := findCharacterByName(r.deps, divisionID, playerName)
	targetSession, online := r.sessionByName(divisionID, playerName)
	if target == nil || target.DeletePending || !online || target.ID == actor.ID {
		return false
	}
	if !enterworld.JobsMayParty(enterworld.PartyJobClass(actor), enterworld.PartyJobClass(target)) {
		return false
	}
	actorID := enterworld.ObjectIDForCharacter(actor)
	if snapshot, inParty := r.registry.PartyOf(divisionID, target.Name); inParty {
		for _, member := range snapshot.Members {
			if member.MemberID == actorID {
				return true
			}
		}
		if !enterworld.JobsMayParty(r.partyJobClass(divisionID, snapshot, target), enterworld.PartyJobClass(actor)) {
			return false
		}
		if _, actorInParty := r.registry.PartyOf(divisionID, actor.Name); actorInParty {
			r.dropMember(divisionID, actor.Name, PartyLeaveReasonSecede, nil)
		}
		joined, refusal := r.registry.Join(divisionID, target.Name, Member{MemberID: actorID, Name: actor.Name})
		if refusal != "" {
			return false
		}
		joinRow := r.memberRowFor(divisionID, actor)
		for _, member := range joined.Members {
			if member.MemberID == actorID {
				continue
			}
			if peer, peerOnline := r.sessionByName(divisionID, member.Name); peerOnline {
				_ = peer.Send(OpPartyUpdate, EncodePartyJoin3E58(joinRow))
			}
		}
		if r.PartyChanged != nil {
			for _, member := range joined.Members {
				r.PartyChanged(divisionID, member.Name)
			}
		}
		return true
	}
	if actorSnap, actorInParty := r.registry.PartyOf(divisionID, actor.Name); actorInParty {
		if !r.partyHasHumanMember(divisionID, actorSnap.Members) {
			r.dropMember(divisionID, actor.Name, PartyLeaveReasonSecede, nil)
		}
	}
	if _, actorInParty := r.registry.PartyOf(divisionID, actor.Name); !actorInParty {
		// Port-only, not native: the player consented by hiring (the hire
		// window's confirm, or !hire/!accept), so no prompt follows. The
		// hirer forms the party and leads it, so it can put an agent out
		// or disband the party, which ends that hire (caravanai).
		leader := Member{MemberID: enterworld.ObjectIDForCharacter(target), Name: target.Name}
		second := Member{MemberID: actorID, Name: actor.Name}
		opts := uint8(PartyOptionExpShare | PartyOptionItemShare | PartyOptionJoinAnyone)
		snapshot, refusal := r.registry.Form(divisionID, leader, second, opts)
		if refusal != "" {
			return false
		}
		sendPartySeed(targetSession, leader.MemberID, snapshot, r.rosterRows(divisionID, snapshot))
		if r.PartyChanged != nil {
			for _, member := range snapshot.Members {
				r.PartyChanged(divisionID, member.Name)
			}
		}
		return true
	}
	if r.proposalPending(divisionID, target.Name) {
		return false
	}
	if actorSnap, actorInParty := r.registry.PartyOf(divisionID, actor.Name); actorInParty {
		r.registry.SetPendingInvite(divisionID, target.Name, PendingInvite{
			Kind:        PendingInviteJoin,
			InviterName: actor.Name,
			OptionBits:  actorSnap.OptionBits,
		})
		_ = targetSession.Send(OpInvitationProposal, EncodeInvitationPrompt3393(InvitationTypePartyJoin, actorID, actorSnap.OptionBits))
	}
	return true
}

/*
==================
MemberNames

The names in playerName's party, in member order (nil outside a party).
==================
*/
func (r *Runtime) MemberNames(divisionID, playerName string) []string {
	if r == nil {
		return nil
	}
	snapshot, ok := r.registry.PartyOf(divisionID, playerName)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		names = append(names, member.Name)
	}
	return names
}

/*
==================
RemoveRuntimeActor

A hired runtime actor whose hire ended leaves its party.
==================
*/
func (r *Runtime) RemoveRuntimeActor(divisionID, actorName string) {
	if r == nil {
		return
	}
	actor := findCharacterByName(r.deps, divisionID, actorName)
	if actor == nil || !actor.RuntimeOnly {
		return
	}
	if _, inParty := r.registry.PartyOf(divisionID, actorName); inParty {
		r.dropMember(divisionID, actorName, PartyLeaveReasonSecede, nil)
	}
}

func (r *Runtime) partyHasHumanMember(divisionID string, members []Member) bool {
	for _, member := range members {
		c := findCharacterByName(r.deps, divisionID, member.Name)
		if c != nil && !c.RuntimeOnly {
			return true
		}
	}
	return false
}

/*
================
memberPosition

A member row's position: a runtime-only (AI caravan) actor's live pose,
since it is never saved; a player's saved world spawn otherwise.
================
*/
func (r *Runtime) memberPosition(divisionID string, character *enterworld.Character) (uint16, int16, int16, int16) {
	if character.RuntimeOnly && r.livePose != nil {
		pose := r.livePose(divisionID, character)
		return pose.RegionID, int16(pose.X), int16(pose.Y), int16(pose.Z)
	}
	world := enterworld.WorldStateForCharacter(character, enterworld.ResolveCharacterRaceKey(character))
	return uint16(world.Spawn.RegionID), int16(world.Spawn.X), int16(world.Spawn.Y), int16(world.Spawn.Z)
}
