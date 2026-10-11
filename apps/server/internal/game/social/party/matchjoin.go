/*
===========================================================================

matchjoin.go - roster changes requested by party matching

===========================================================================
*/

package party

// The party-match JOIN seam: internal/game/social/match's owner-approval handshake
// (0x75BF -> owner notify -> 0x30FA answer) ends in a ROSTER change,
// and rosters belong to THIS lane - so match reaches these three
// methods through wiring.go func fields (the MemberCountFor posture;
// internal/game/social/match never imports internal/game/social/party). Nothing here registers on the
// hub and nothing here holds new state: the registry re-validates every
// commit under its own lock exactly like the 0x3393 consent commits.

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
)

/*
==================
MaskedMemberInfoFor

MaskedMemberInfoFor renders one character's masked member-info record
(the sub_75db30 wire shape, mask 0x37 full-info subset) off the live
character record - the tail of the match lane's 0x75BF owner notify.
ok=false when the name resolves to no division character.
==================
*/
func (r *Runtime) MaskedMemberInfoFor(divisionID, name string) ([]byte, bool) {
	character := findCharacterByName(r.deps, divisionID, name)
	if character == nil {
		return nil, false
	}
	return EncodeMaskedMemberRow(r.memberRowFor(divisionID, character)), true
}

/*
==================
MatchJoinPrecheck

MatchJoinPrecheck answers a request-time refusal reason ("" = ok) for
a proposed match join: the joiner must be partyless, and the owner -
when already partied - must lead a non-full party (the client gates
listing REGISTRATION on leadership the same way, sub_6356a0
@0x63577d, so a non-leader owner is a stale listing). Everything is
re-validated at commit time; this precheck only spares the owner a
prompt that could never commit.
==================
*/
func (r *Runtime) MatchJoinPrecheck(divisionID, ownerName, joinerName string) string {
	if _, partied := r.registry.PartyOf(divisionID, joinerName); partied {
		return "joiner already in a party"
	}
	snapshot, partied := r.registry.PartyOf(divisionID, ownerName)
	if !partied {
		return ""
	}
	owner := findCharacterByName(r.deps, divisionID, ownerName)
	if owner == nil {
		return "owner unresolvable"
	}
	if snapshot.LeaderID != enterworld.ObjectIDForCharacter(owner) {
		return "owner no longer leads their party"
	}
	if len(snapshot.Members) >= partyCapacity(snapshot.OptionBits) {
		return "owner's party is full"
	}
	return ""
}

/*
==================
AdmitMatchJoin

AdmitMatchJoin commits one ACCEPTED match join: the owner partyless
FORMS a two-member party (leader = owner; optionBits = the LISTING's
type bits, which the registering client folded from the live
party-settings words - sub_63bba0 @0x0063bc2c..0x0063bc53 - so the
listing is the only bit source that exists), the owner partied JOINS
the joiner into their roster. Both paths run the EXISTING consent
commit legs, so the pinned frames (0xB0D5 + 0x35D6 seeds, the 0x3E58
type-2 join row toward sitting members) ride unchanged. Returns the
refusal reason ("" = committed).
==================
*/
func (r *Runtime) AdmitMatchJoin(divisionID, ownerName, joinerName string, optionBits uint8) string {
	owner := findCharacterByName(r.deps, divisionID, ownerName)
	if owner == nil {
		return "owner unresolvable"
	}
	joiner := findCharacterByName(r.deps, divisionID, joinerName)
	if joiner == nil {
		return "joiner unresolvable"
	}
	ownerSession, ownerOnline := r.presence.SessionByName(divisionID, owner.Name)
	if !ownerOnline {
		return "owner logged off"
	}
	joinerSession, joinerOnline := r.presence.SessionByName(divisionID, joiner.Name)
	if !joinerOnline {
		return "joiner logged off"
	}
	// ShardManager 44F850 (0x7C16) tests the joiner against the party's job
	// class (44ED20) when the join is requested; the commit repeats it, as
	// the consent commits do, so a suit changed meanwhile cannot slip in.
	if !enterworld.JobsMayParty(r.matchPartyJobClass(divisionID, owner), enterworld.PartyJobClass(joiner)) {
		return "opposing job classes"
	}
	if _, partied := r.registry.PartyOf(divisionID, owner.Name); !partied {
		leader := Member{MemberID: enterworld.ObjectIDForCharacter(owner), Name: owner.Name}
		second := Member{MemberID: enterworld.ObjectIDForCharacter(joiner), Name: joiner.Name}
		snapshot, refusal := r.registry.Form(divisionID, leader, second, optionBits&PartyOptionMask)
		if refusal != "" {
			return refusal
		}
		rows := r.rosterRows(divisionID, snapshot)
		sendPartySeed(ownerSession, leader.MemberID, snapshot, rows)
		sendPartySeed(joinerSession, second.MemberID, snapshot, rows)
		log.Debugf("party: %s formed a party with %s through the match board (options 0x%02X)", owner.Name, joiner.Name, snapshot.OptionBits)
		return ""
	}
	snapshot, _ := r.registry.PartyOf(divisionID, owner.Name)
	if snapshot.LeaderID != enterworld.ObjectIDForCharacter(owner) {
		return "owner no longer leads their party"
	}
	joinerMember := Member{MemberID: enterworld.ObjectIDForCharacter(joiner), Name: joiner.Name}
	joined, refusal := r.registry.Join(divisionID, owner.Name, joinerMember)
	if refusal != "" {
		return refusal
	}
	joinRow := r.memberRowFor(divisionID, joiner)
	for _, member := range joined.Members {
		if member.MemberID == joinerMember.MemberID {
			continue
		}
		if peer, online := r.presence.SessionByName(divisionID, member.Name); online {
			_ = peer.Send(OpPartyUpdate, EncodePartyJoin3E58(joinRow))
		}
	}
	sendPartySeed(joinerSession, joinerMember.MemberID, joined, r.rosterRows(divisionID, joined))
	log.Debugf("party: %s joined %s's party through the match board (%d member(s))", joiner.Name, owner.Name, len(joined.Members))
	return ""
}

// ListingAuthority reads the same roster owner used by admission and consent.
func (r *Runtime) ListingAuthority(divisionID, name string) (uint8, bool, bool) {
	snapshot, partied := r.registry.PartyOf(divisionID, name)
	if !partied {
		return 0, false, false
	}
	character := findCharacterByName(r.deps, divisionID, name)
	return snapshot.OptionBits, true, character != nil && snapshot.LeaderID == enterworld.ObjectIDForCharacter(character)
}

/*
================
matchPartyJobClass

The class a match join compares against: the owner's party's (its
leader's), or the owner's own when the join forms the party.
================
*/
func (r *Runtime) matchPartyJobClass(divisionID string, owner *enterworld.Character) uint8 {
	if snapshot, partied := r.registry.PartyOf(divisionID, owner.Name); partied {
		return r.partyJobClass(divisionID, snapshot, owner)
	}
	return enterworld.PartyJobClass(owner)
}
