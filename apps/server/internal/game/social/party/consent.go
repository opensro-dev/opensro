/*
===========================================================================

consent.go - resolve party proposals and report every terminal outcome.

Party formation and joining share one reply owner. A consumed invitation
must either publish membership or notify the surviving participants.

===========================================================================
*/
package party

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

const (
	partyFailureUnknown        byte = 0x02
	partyFailureCreatorMissing byte = 0x0e
	partyFailureTimeout        byte = 0x10
	partyFailureFormRefused    byte = 0x0c
	partyFailureJoinRefused    byte = 0x17
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

5BE9B0/5BEA20 fan out to both surviving peers; B452 is the invitee carrier.
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
	if inviter == nil || !online {
		// 5BE45B / 5BE3E3 report 2C0E when the proposer has disappeared.
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
	leader := Member{MemberID: enterworld.ObjectIDForCharacter(inviter), Name: inviter.Name}
	second := Member{MemberID: enterworld.ObjectIDForCharacter(target), Name: target.Name}
	snapshot, refusal := r.registry.Form(divisionID, leader, second, optionBits)
	if refusal != "" {
		// Inference: local registry rejection replaces a failed coordinator job;
		// use its generic failure (5BE447 / 5BE3BB), never silent success.
		return partyFailureUnknown
	}
	rows := r.rosterRows(divisionID, snapshot)
	sendPartySeed(inviterSession, leader.MemberID, snapshot, rows)
	sendPartySeed(targetSession, second.MemberID, snapshot, rows)
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
seed (which also clears the inviter's pending latch, the native
@0x0076193e leg).
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
	joiner := Member{MemberID: enterworld.ObjectIDForCharacter(target), Name: target.Name}
	joined, refusal := r.registry.Join(divisionID, inviter.Name, joiner)
	if refusal != "" {
		// Inference: local registry rejection replaces a failed coordinator job;
		// use its generic failure (5BE447 / 5BE3BB), never silent success.
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
	sendPartySeed(targetSession, joiner.MemberID, joined, r.rosterRows(divisionID, joined))
	return 0
}
