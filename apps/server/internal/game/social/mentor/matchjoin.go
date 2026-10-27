/*
===========================================================================

matchjoin.go - a mentor-match join committed into the master's camp

The match board's accepted join admits the student through the same
store door and 0x3AC5 fan-out as an accepted invitation.

===========================================================================
*/
package mentor

// The mentor-match JOIN seam: internal/game/social/match's owner-approval handshake
// (0x7592 -> kind-0xe owner prompt -> 0x35D5 answer) ends in a CAMP
// MEMBERSHIP change, and camps belong to THIS lane - so match reaches
// the two methods below through wiring.go func fields (the party lane's
// MemberCountFor posture; internal/game/social/match never imports internal/game/social/mentor).
//
// This file is the match lane's ONE seam into the relationship commit:
// it holds NO state of its own, runs the SAME atomic AdmitStudent
// door and the SAME 0x3AC5 fan-out helpers as the 0x76B1
// invite consent (invite.go ApplyConsent is the canonical shape - the
// commit below deliberately mirrors its accept tail over the shared
// primitives instead of introducing a second camp mutator). The
// direction differs only in WHO initiates: 0x76B1 is master->student,
// the match join is student->master with the master's 0x35D5 approval
// standing where the 0x3393 {01 01} consent stood.
//
// DECISION (listing-kind mapping): the joiner commits as a kind-2
// STUDENT. The mentor-match register form's kind combo (Student /
// AssistantGuardian / ALL, the matching window's OnCreate) is not
// pinned to a member-kind byte in either dump, and the join button's
// own gate (the client's student-band gate: level < 0x3c) admits
// both bands - the
// student band is the only one with a pinned level ceiling
// (StudentMaxLevel), so it is the honest commit until the kind byte's
// semantics are pinned.

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

/*
================
MatchJoinPrecheck

MatchJoinPrecheck answers a request-time refusal reason ("" = ok) for
a proposed mentor-match join, mirroring the 0x76B1 invite gates
(handleInvite): the master must be recognizable (level >= 0x3c - the
TrainingCampMember_ClassifyGrade kind-0 gate - and the kind-0
master of a camp opened at the
NPC, with student headroom), the joiner campless, not delete-pending and
inside the student band. Everything re-validates at commit time under
the store doors.
================
*/
func (r *InviteRuntime) MatchJoinPrecheck(divisionID, masterName, joinerName string) string {
	if r.deps.TrainingCampAuthority() == nil {
		return "no training-camp store wired"
	}
	master := findCampCharacterByName(r.deps, divisionID, masterName)
	if master == nil {
		return "master unresolvable"
	}
	if characterLevel(master) < MasterMinLevel {
		return "master below the 0x3c band"
	}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, master.ID)
	if !joined {
		return "master has no camp opened"
	}
	camp, members, ok := r.deps.TrainingCampAuthority().Camp(divisionID, campID)
	if !ok {
		return "master's camp resolves to no stored row"
	}
	if camp.MasterCharID != master.ID {
		return "listing owner is not their camp's master"
	}
	if countCampKind(members, MemberKindStudent) >= CampStudentCap {
		return "camp sits at the student cap"
	}
	joiner := findCampCharacterByName(r.deps, divisionID, joinerName)
	if joiner == nil {
		return "joiner unresolvable"
	}
	if joiner.DeletePending {
		return "joiner is delete-pending"
	}
	if _, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, joiner.ID); joined {
		return "joiner already sits in a camp"
	}
	if characterLevel(joiner) > StudentMaxLevel {
		return "joiner over the student band"
	}
	return ""
}

/*
================
CommitMatchJoin

CommitMatchJoin commits one ACCEPTED mentor-match join: the joiner's
kind-2 student row installs through the atomic admission door into the
master's opened camp, then the 0x3AC5 fan-out runs exactly like the
invite consent's: the joiner gets their status-10 sub-1 seed on
joinerSession and every sitting online member the status-2 join row.
Returns the refusal reason ("" = committed).
================
*/
func (r *InviteRuntime) CommitMatchJoin(joinerSession *transport.Session, divisionID, masterName, joinerName string) string {
	if refusal := r.MatchJoinPrecheck(divisionID, masterName, joinerName); refusal != "" {
		return refusal
	}
	master := findCampCharacterByName(r.deps, divisionID, masterName)
	joiner := findCampCharacterByName(r.deps, divisionID, joinerName)
	if master == nil || joiner == nil {
		return "participants unresolvable"
	}
	if !r.presence.OnlineByName(divisionID, master.Name) {
		return "master logged off"
	}
	joinerRow := enterworld.TrainingCampMemberRecord{CharID: joiner.ID, Kind: MemberKindStudent, JoinLevel: characterLevel(joiner)}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, master.ID)
	if !joined {
		return "master has no camp opened"
	}
	admission, ok := r.deps.TrainingCampAuthority().AdmitStudent(
		divisionID,
		enterworld.TrainingCampAdmission{
			MasterCharID:    master.ID,
			StudentCharID:   joiner.ID,
			ExpectedCampID:  campID,
			MasterMinLevel:  MasterMinLevel,
			StudentMaxLevel: StudentMaxLevel,
			StudentLimit:    CampStudentCap,
			NowMs:           r.Now().UnixMilli(),
		},
	)
	if !ok {
		return "the training-camp admission door refused stale or ineligible state"
	}
	campID = admission.Camp.ID
	campMembers := admission.Members
	rows := r.campWireRows(divisionID, campMembers)
	joinerWireRow := r.memberWireRow(divisionID, joinerRow)
	_ = joinerSession.Send(OpTCStatus, EncodeCampSeed3AC5WithEvaluation(joinerWireRow.MemberID, admission.Camp.EvaluationRank, admission.Camp.Subject, admission.Camp.Contents, rows))
	joinPush := EncodeCampJoinRow3AC5(joinerWireRow)
	for _, member := range campMembers {
		if member.CharID == joiner.ID {
			continue
		}
		character := r.findDivisionCharacterByID(divisionID, member.CharID)
		if character == nil {
			continue
		}
		peer, live := r.presence.SessionByName(divisionID, character.Name)
		if !live {
			continue
		}
		_ = peer.Send(OpTCStatus, joinPush)
	}
	log.Debugf("mentor: %s joined %s's academy through the match board (camp %d, %d member(s))", joiner.Name, master.Name, campID, len(campMembers))
	return ""
}
