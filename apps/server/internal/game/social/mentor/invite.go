/*
===========================================================================

invite.go - mentor invitations

===========================================================================
*/

package mentor

// The mentor/TC INVITE HANDSHAKE (backlog T62): 0x76B1 holds a pending
// invite and prompts the target with 0x3393 {u8 9, u32 inviterGid,
// u16-len name} (the CPSMission_OnConfirmBoxPrompt0x3393 IDCLOSE
// arm); camp membership commits ONLY on the target's 0x3393 {01 01}
// accept (CGInterface_DispatchSimpleMessageBoxResult case 7)
// through the ATOMIC AdmitStudent door, then the joiner
// gets their own 0x3AC5 status-10 sub-1 seed and every sitting online
// member the status-2 join row. The party lane owns the single 0x3393
// hub registration and routes replies here by pending-invite ownership
// (party.ConsentArm, implemented structurally like the guild arm);
// wiring.go hooks this runtime in and extends the cross-lane pending
// dismissal in all directions. Refusals stay SILENT except the ONE
// pinned-carrier notice: the inviter's 0x3AC5 {10, 2, 0x11} rejection
// notice (wire.go NoticeInviteRejection).

import (
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// Presence is the single cross-session lookup this lane consumes. The
// interface stays at the consumer boundary so the lane owns no session map.
type Presence interface {
	// SessionByName resolves the live session driving a character, or
	// (nil, false) when the character is offline.
	SessionByName(divisionID, name string) (*transport.Session, bool)
	// OnlineByName reports whether a character has a live bound session.
	OnlineByName(divisionID, name string) bool
}

/*
==================
PendingInvite

PendingInvite is one outstanding mentor/TC invitation: stored when the
0x76B1 proposal sends the 0x3393 type-9 prompt, consumed by the
target's consent. Everything is re-validated at consent time - the
camp world may have changed while the prompt was up.
==================
*/
type PendingInvite struct {
	// InviterName resolves the inviting master at consent time (the
	// party lane's shape - never a session pointer).
	InviterName string
	// CampID is the camp the prompt proposed - 0 when the master had no
	// camp yet (the camp is then CREATED atomically with the commit,
	// the wire.go camp-creation DECISION). A master who gained a
	// DIFFERENT camp mid-prompt invalidates the invitation.
	CampID int64

	expiresAtMs int64
}

/*
==================
InviteRuntime

InviteRuntime owns the mentor lane's pending-invitation table - the
ONLY in-memory state this lane holds (camp membership itself is
store-persisted; invitations are session-scoped runtime state that
legitimately dies on process reboot, the guild lane's verdict).
==================
*/
type InviteRuntime struct {
	deps     Dependencies
	presence Presence

	mu              sync.Mutex
	pendingByTarget map[string]PendingInvite

	// PeerPending reports another lane's unanswered proposal for a
	// player. A player holds one at a time (the native transaction
	// manager), so this lane then refuses. The type-9 prompt is a plain
	// message box outside the client's prompt-retire leg (@0x00764625),
	// which is why a reply is only routable while one lane holds a
	// pending. wiring.go points it at the party registry and the other
	// lanes (a func field, not an import).
	PeerPending func(divisionID, name string) bool

	// LevelData supplies level 40's Exp_C curve row to CalcGraduateBonusExp.
	LevelData enterworld.LevelDataSource

	// GrantGraduateExperience applies the graduate bonus EXP to the
	// graduating student and publishes the resulting 0x30D2 / level-up frames.
	GrantGraduateExperience func(divisionID string, charID int64, expDelta int64)

	// SyncMasterHonorBuffs reconciles a camp master's active Honor Rank
	// skill buffs (20160..20163) against their camp's EvaluationRank.
	SyncMasterHonorBuffs func(divisionID string, charID int64, evaluationRank uint8)

	// Now is the clock the 30 s answer window runs on.
	Now func() time.Time
}

/*
==================
NewInviteRuntime

NewInviteRuntime builds the runtime over an empty pending table and retains
the shared deps pointer. Construct it before lifecycle closures capture the
runtime itself.
==================
*/
func NewInviteRuntime(deps Dependencies, presence Presence) *InviteRuntime {
	return &InviteRuntime{
		deps:            deps,
		presence:        presence,
		pendingByTarget: make(map[string]PendingInvite),
		Now:             time.Now,
	}
}

// inviteAnswerWindowMs is the transaction timeout (the native
// transaction stores 30 s); natively a proposal expires after it, not at it.
const inviteAnswerWindowMs = 30 * 1000

/*
================
expired

expired reports an invitation past its answer window. The caller holds mu.
================
*/
func (r *InviteRuntime) expired(invite PendingInvite) bool {
	return r.Now().UnixMilli() > invite.expiresAtMs
}

/*
================
inviteKey

inviteKey is the pending table's target identity: divisionID + ":" +
lowercase(name) - the hub bind-key convention every lane keys by.
================
*/
func inviteKey(divisionID, name string) string {
	return divisionID + ":" + strings.ToLower(name)
}

/*
==================
Register

Register wires academy invitation and notice editing onto the hub. The
0x3393 consent reply deliberately does NOT register here - the party
lane owns that shared registration (last-write-wins hub registration
means a second Handle(0x3393) would BREAK party and guild consent).
==================
*/
func (r *InviteRuntime) Register(hub *transport.Hub) {
	hub.Handle(OpTCInviteRequest, r.handleInvite)
	hub.Handle(OpTCNoticeEditRequest, r.handleNoticeEdit)
	hub.Handle(OpTCOpenRequest, r.handleOpen)
	hub.Handle(OpTCDisbandRequest, r.handleDisband)
	hub.Handle(OpTCLeaveRequest, r.handleLeave)
	hub.Handle(OpTCKickRequest, r.handleKick)
	hub.Handle(OpTCGraduateRequest, r.handleGraduate)
	hub.Handle(OpTCCompulsoryGraduateRequest, r.handleCompulsoryGraduate)
	hub.Handle(OpTCHonorRankRequest, r.handleHonorRank)
}

/*
================
PendingInviteCount

PendingInviteCount reports the number of outstanding invitations (the
reboot-empty and leak assertions).
================
*/
func (r *InviteRuntime) PendingInviteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendingByTarget)
}

/*
================
setPending

setPending records the outstanding invitation for a target and starts
its answer window. handleInvite never calls it over a live proposal.
================
*/
func (r *InviteRuntime) setPending(divisionID, targetName string, invite PendingInvite) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite.expiresAtMs = r.Now().UnixMilli() + inviteAnswerWindowMs
	r.pendingByTarget[inviteKey(divisionID, targetName)] = invite
}

/*
==================
takePending

takePending consumes the target's outstanding invitation. A consent
with no pending record (never invited, already answered, or a
duplicate/stale frame) reports false and the caller drops silently.
==================
*/
func (r *InviteRuntime) takePending(divisionID, targetName string) (PendingInvite, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inviteKey(divisionID, targetName)
	invite, ok := r.pendingByTarget[key]
	if ok {
		delete(r.pendingByTarget, key)
	}
	return invite, ok && !r.expired(invite)
}

/*
==================
HasPendingInvite

HasPendingInvite reports whether an invitation targets the character -
the party lane's consent router asks it to decide reply ownership
(party.ConsentArm, implemented structurally).
==================
*/
func (r *InviteRuntime) HasPendingInvite(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite, ok := r.pendingByTarget[inviteKey(divisionID, name)]
	return ok && !r.expired(invite)
}

/*
==================
DropPendingInvite

DropPendingInvite clears the invitation targeting a character when their
session ends. Reports whether one dropped.
==================
*/
func (r *InviteRuntime) DropPendingInvite(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inviteKey(divisionID, name)
	if _, ok := r.pendingByTarget[key]; !ok {
		return false
	}
	delete(r.pendingByTarget, key)
	return true
}

/*
==================
findCampCharacterByGid

findCampCharacterByGid resolves an invite target's world gid back onto
the division character record (the guild lane's twin, duplicated
because the import points the other way).
==================
*/
func findCampCharacterByGid(deps Dependencies, divisionID string, gid uint32) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && enterworld.ObjectIDForCharacter(candidate) == gid {
			return characterSnapshot(deps, divisionID, candidate)
		}
	}
	return nil
}

/*
================
findCampCharacterByName

findCampCharacterByName resolves a division character record by name,
case-insensitively (the guild lane's twin).
================
*/
func findCampCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
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
door is held. Camp policy may combine persisted relationship rows with
character level/deletion state, so it must never retain a live record.
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
================
characterLevel

characterLevel reads the persisted level with the store's >=1 floor
and the wire's u8 ceiling.
================
*/
func characterLevel(c *enterworld.Character) uint8 {
	if c == nil || c.Level == nil || *c.Level < 1 {
		return 1
	}
	if *c.Level > 0xFF {
		return 0xFF
	}
	return uint8(*c.Level)
}

/*
==================
memberWireRow

memberWireRow projects one persisted camp member onto its 0x3AC5 wire
row through the live character record: the member id is the world gid
(the party MemberID posture), name and level project live so they can
never rot, and every unpinned-semantics field stays zero (wire.go).
A member whose record vanished mid-flight degrades to the bare row.
==================
*/
func (r *InviteRuntime) memberWireRow(divisionID string, member enterworld.TrainingCampMemberRecord) MemberWireRow {
	row := MemberWireRow{Kind: member.Kind, LevelByte58: 1, Level: 1, HonorPoints: member.HonorPoints}
	character := r.findDivisionCharacterByID(divisionID, member.CharID)
	if character == nil {
		return row
	}
	row.MemberID = enterworld.ObjectIDForCharacter(character)
	row.Name = character.Name
	level := characterLevel(character)
	row.LevelByte58 = level
	if member.JoinLevel > 0 {
		row.LevelByte58 = member.JoinLevel
	}
	row.Level = level
	return row
}

/*
================
findDivisionCharacterByID

findDivisionCharacterByID resolves a division character record by id.
================
*/
func (r *InviteRuntime) findDivisionCharacterByID(divisionID string, characterID int64) *enterworld.Character {
	for _, candidate := range r.deps.CharactersForDivision(divisionID) {
		if candidate != nil && candidate.ID == characterID {
			return characterSnapshot(r.deps, divisionID, candidate)
		}
	}
	return nil
}

/*
================
campWireRows

campWireRows projects a camp's whole member set in store order (the
wire order the seed roster emits).
================
*/
func (r *InviteRuntime) campWireRows(divisionID string, members []enterworld.TrainingCampMemberRecord) []MemberWireRow {
	rows := make([]MemberWireRow, 0, len(members))
	for _, member := range members {
		rows = append(rows, r.memberWireRow(divisionID, member))
	}
	return rows
}

/*
================
countCampKind

countCampKind tallies one member kind across a camp's member set.
================
*/
func countCampKind(members []enterworld.TrainingCampMemberRecord, kind uint8) int {
	count := 0
	for _, member := range members {
		if member.Kind == kind {
			count++
		}
	}
	return count
}

/*
==================
handleInvite

handleInvite applies one 0x76B1 proposal: the actor must be a
recognizable camp MASTER (level >= 0x3c - the
TrainingCampMember_ClassifyGrade kind-0 gate
the client's own master-is-me path depends on - and the kind-0 master
of a camp opened at the NPC, with student headroom), the target
must resolve by world gid within the actor's division, be someone
else, not delete-pending, ONLINE (the prompt needs a live session),
campless and inside the student band. Then the target gets the 0x3393
type-9 prompt and the proposal parks in the pending table - NOTHING
commits here; the membership moves only when the target's accept
consent arrives. Every refusal stays silent
(no invite-request refusal carrier is pinned toward the actor).
==================
*/
func (r *InviteRuntime) handleInvite(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		log.Debugf("mentor: 0x%04X (invite) from unbound session %d discarded", opcode, s.ID)
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending {
		log.Debugf("mentor: 0x%04X (invite) from unavailable character on session %d discarded", opcode, s.ID)
		return
	}
	targetRef, err := DecodeInviteRequest(payload)
	if err != nil {
		log.Debugf("mentor: 0x76B1 (invite) malformed from %s: %v", actor.Name, err)
		return
	}
	if r.deps.TrainingCampAuthority() == nil {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: no training-camp store wired", actor.Name)
		return
	}
	if characterLevel(actor) < MasterMinLevel {
		// Below 0x3c the client's TrainingCampMember_ClassifyGrade kind-0 leg never publishes
		// the master name, so the inviter's own pane could not have
		// armed the Invite button - a frame that arrives anyway is a
		// desync or forged.
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: level %d below the 0x3c master band", actor.Name, characterLevel(actor))
		return
	}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, actor.ID)
	if !joined {
		// A camp is opened at the storage NPC (open.go). The client's
		// Invite button needs master-is-me (in
		// CIFApprenticeShip_OnInviteBtn), which only a camp seed arms,
		// so a campless frame is a desync or forged.
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: no camp opened", actor.Name)
		return
	}
	camp, members, ok := r.deps.TrainingCampAuthority().Camp(divisionID, campID)
	if !ok {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: camp %d resolves to no stored row", actor.Name, campID)
		return
	}
	if camp.MasterCharID != actor.ID {
		// A sub-mentor/student cannot invite - the client gates the
		// button on master-is-me (in CIFApprenticeShip_OnInviteBtn).
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: not the master of camp %d", actor.Name, campID)
		return
	}
	if countCampKind(members, MemberKindStudent) >= CampStudentCap {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: camp %d sits at the %d-student cap", actor.Name, campID, CampStudentCap)
		return
	}
	target := findCampCharacterByGid(r.deps, divisionID, targetRef)
	if target == nil {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: target gid %d not in the division", actor.Name, targetRef)
		return
	}
	if target.ID == actor.ID {
		// The client refuses a self-target before composing (the
		// NetClient_SendTargetCommand702C90 resolve requires target !=
		// data_cedb54).
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: cannot invite yourself", actor.Name)
		return
	}
	if target.DeletePending {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: target %s is delete-pending", actor.Name, target.Name)
		return
	}
	if _, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, target.ID); joined {
		// The cat-0x1D table carries ALREADY_ENTRY / OTHER_TRAININGCAMP_
		// ENTRY codes (0x0F/0x12) but no pinned trigger toward the
		// actor - silent.
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: target %s already sits in a camp", actor.Name, target.Name)
		return
	}
	if characterLevel(target) > StudentMaxLevel {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: target %s level %d over the 0x28 student band", actor.Name, target.Name, characterLevel(target))
		return
	}
	targetSession, online := r.presence.SessionByName(divisionID, target.Name)
	if !online {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: target %s is offline", actor.Name, target.Name)
		return
	}
	// One unanswered proposal per player (as natively). Native fails this one
	// on 0xB472 {2, 2}; no v1.150 carrier is pinned, so it stays silent.
	if r.HasPendingInvite(divisionID, target.Name) || r.PeerPending != nil && r.PeerPending(divisionID, target.Name) {
		log.Debugf("mentor: 0x76B1 (invite) refused for %s: %s already has a proposal waiting", actor.Name, target.Name)
		return
	}
	r.setPending(divisionID, target.Name, PendingInvite{
		InviterName: actor.Name,
		CampID:      campID,
	})
	_ = targetSession.Send(OpInvitationProposal, EncodeInvitePrompt3393(enterworld.ObjectIDForCharacter(actor), actor.Name))
	log.Debugf("mentor: %s proposed academy membership to %s (camp %d) - type-9 prompt sent", actor.Name, target.Name, campID)
}

/*
==================
ApplyConsent

ApplyConsent resolves one routed 0x3393 reply against the pending
table (the party lane's router hands replies here when THIS lane
holds the target's pending invitation). Every edge stays SILENT on
the wire except the ONE pinned-carrier notice:

  - no outstanding invitation -> drop;
  - anything but the exact {01 01} accept pair (the {02 00} refuse,
    or any forged shape) -> the pending invitation is consumed and
    the ONLINE inviter gets the 0x3AC5 {10, 2, 0x11} rejection
    notice (UIIT_MSG_TC_ERROR_INVITE_REJECTION - the trigger pairing
    is the wire.go DECISION);
  - inviter logged off / no longer resolvable, the proposed camp
    gone or re-mastered, caps reached, target camped or over the
    band meanwhile -> refused silently, and the admission door
    re-validates every membership, lifecycle, level, and capacity edge.

On the committed accept the joiner receives their own 0x3AC5
status-10 sub-1 seed (their client holds no camp state) and every OTHER
sitting online member, the master included, the status-2 join row.
==================
*/
func (r *InviteRuntime) ApplyConsent(s *transport.Session, divisionID string, actor *enterworld.Character, result, code uint8) {
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending {
		return
	}
	invite, outstanding := r.takePending(divisionID, actor.Name)
	if !outstanding {
		log.Debugf("mentor: 0x3393 consent from %s dropped: no outstanding TC invitation", actor.Name)
		return
	}
	if result != ConsentResultAccept || code != ConsentCodeAccept {
		log.Debugf("mentor: %s refused %s's TC invitation ({%d %#x})", actor.Name, invite.InviterName, result, code)
		if inviterSession, online := r.presence.SessionByName(divisionID, invite.InviterName); online {
			_ = inviterSession.Send(OpTCStatus, EncodeCampNotice3AC5(NoticeInviteRejection))
		}
		return
	}
	if r.deps.TrainingCampAuthority() == nil {
		log.Debugf("mentor: 0x3393 accept from %s dropped: no training-camp store wired", actor.Name)
		return
	}
	inviter := findCampCharacterByName(r.deps, divisionID, invite.InviterName)
	if inviter == nil {
		log.Debugf("mentor: 0x3393 accept from %s dropped: inviter %s no longer resolvable", actor.Name, invite.InviterName)
		return
	}
	if !r.presence.OnlineByName(divisionID, inviter.Name) {
		// DECISION (the party/guild consent posture): an inviter who
		// logged off mid-prompt invalidates the invitation.
		log.Debugf("mentor: 0x3393 accept from %s dropped: inviter %s logged off", actor.Name, invite.InviterName)
		return
	}
	if characterLevel(actor) > StudentMaxLevel {
		log.Debugf("mentor: 0x3393 accept from %s dropped: level %d over the student band", actor.Name, characterLevel(actor))
		return
	}
	joinerRow := enterworld.TrainingCampMemberRecord{CharID: actor.ID, Kind: MemberKindStudent, JoinLevel: characterLevel(actor)}
	admission, ok := r.deps.TrainingCampAuthority().AdmitStudent(
		divisionID,
		enterworld.TrainingCampAdmission{
			MasterCharID:    inviter.ID,
			StudentCharID:   actor.ID,
			ExpectedCampID:  invite.CampID,
			MasterMinLevel:  MasterMinLevel,
			StudentMaxLevel: StudentMaxLevel,
			StudentLimit:    CampStudentCap,
			NowMs:           r.Now().UnixMilli(),
		},
	)
	if !ok {
		log.Debugf("mentor: 0x3393 accept from %s not committed: the admission door refused stale or ineligible state", actor.Name)
		return
	}
	campID := admission.Camp.ID
	joined := admission.Members
	rows := r.campWireRows(divisionID, joined)
	joinerWireRow := r.memberWireRow(divisionID, joinerRow)
	_ = s.Send(OpTCStatus, EncodeCampSeed3AC5WithEvaluation(joinerWireRow.MemberID, admission.Camp.EvaluationRank, admission.Camp.Subject, admission.Camp.Contents, rows))
	joinPush := EncodeCampJoinRow3AC5(joinerWireRow)
	for _, member := range joined {
		if member.CharID == actor.ID {
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
	log.Debugf("mentor: %s joined %s's academy (camp %d, %d member(s))", actor.Name, inviter.Name, campID, len(joined))
}

/*
==================
WorldBound

WorldBound is the enter-world hook, called from the server's
OnWorldBound tail on the winner path: a prompt the PREVIOUS session
received died with it (the party/guild session-boundary rule), and a
character who is a persisted camp MEMBER gets their 0x3AC5 status-10
sub-1 camp seed - the fresh client holds no camp state, and the seed
is what arms the Academy pane's in-camp/master-is-me gates (the
siege lane's post-bootstrap seed precedent).
==================
*/
func (r *InviteRuntime) WorldBound(s *transport.Session, divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	if r.DropPendingInvite(divisionID, character.Name) {
		log.Debugf("mentor: %s re-entered the world; stale pending TC invitation dropped", character.Name)
	}
	if r.deps.TrainingCampAuthority() == nil || s == nil {
		return
	}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, character.ID)
	if !joined {
		if r.SyncMasterHonorBuffs != nil {
			r.SyncMasterHonorBuffs(divisionID, character.ID, 0)
		}
		return
	}
	camp, members, ok := r.deps.TrainingCampAuthority().Camp(divisionID, campID)
	if !ok {
		return
	}
	rows := r.campWireRows(divisionID, members)
	_ = s.Send(OpTCStatus, EncodeCampSeed3AC5WithEvaluation(enterworld.ObjectIDForCharacter(character), camp.EvaluationRank, camp.Subject, camp.Contents, rows))
	if r.SyncMasterHonorBuffs != nil {
		evalRank := uint8(0)
		if camp.MasterCharID == character.ID {
			evalRank = camp.EvaluationRank
		}
		r.SyncMasterHonorBuffs(divisionID, character.ID, evalRank)
	}
}

/*
==================
SessionClosed

SessionClosed is the disconnect hook, called from the hub's
OnSessionClose: resolve the closing session's bound character, skip
when ANOTHER session already holds the bind key (the rebind winner is
live - the party lane's loser-close guard), then drop any invitation
targeting them - the prompt died with the target's transport. An
inviter's disconnect deliberately drops nothing here: the
consent-time re-validation refuses a commit whose inviter is gone.
Camp MEMBERSHIP survives the disconnect - it is persisted state.
==================
*/
func (r *InviteRuntime) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.presence.SessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	if r.DropPendingInvite(divisionID, character.Name) {
		log.Debugf("mentor: %s disconnected; pending TC invitation dropped", character.Name)
	}
}
