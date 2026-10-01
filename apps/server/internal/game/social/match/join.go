package match

// The match-JOIN handshake (joinwire.go documents the wire evidence):
//
//	joiner  --0x75BF {entryId}-->  server
//	server  --0x75BF notify----->  listing owner   (request parked)
//	owner   --0x30FA {a,b,ans}-->  server
//	server  --0xB5BF {1,detail}->  joiner          (+ the party frames
//	                                                on an accepted
//	                                                commit, through the
//	                                                internal/game/social/party seam)
//
// (mentor twins: 0x7592 / 0x35D5 / 0xB592, committing through the
// internal/game/social/mentor seam.)
//
// REFUSAL POSTURE: unlike the register/modify/delete legs (whose only
// refusal carrier is the unpinned flag-2 code table, so they stay
// silent), the join request HAS pinned refusal arms the client consumes
// - the 0xB5BF/0xB592 outer-1 details 0 (refused) and 2 (no reply),
// which also clear the joiner's join-progress pane (sub_75ebd0 tail
// @0x0075eda7 -> sub_634eb0(wnd, 0); sub_769ca0 tail @0x00769dae ->
// sub_672280(wnd, 0)). A silent drop would leave that pane latched
// forever, so every server-side refusal of a JOIN answers detail 0, and
// a request that dies without the owner answering (owner logout,
// displacement by a newer request) answers detail 2. DECISION: the
// detail-to-cause mapping beyond "the owner pressed refuse/no-reply" is
// server-chosen - retail's exact cause byte per validation failure is
// unpinned in both dumps.
//
// PENDING-REQUEST TABLE: in-memory and session-scoped like the board
// itself (a reboot drops it; the joiner's client recovers through its
// own no-reply machinery - the owner-side panes carry their own timers,
// e.g. the kind-0xe box's 15s arm @0x005305a11 sub_a00b30(0xa, 0x3a98)).
// One outstanding request per LISTING OWNER: the owner's request pane
// stores exactly one request (sub_63cbc0 overwrites +0x7b8.. in place),
// so a second request would make the first unanswerable client-side -
// the server mirrors that by DISPLACING the older request and acking
// its joiner detail-2 (DECISION). One outstanding request per JOINER
// too: the join-progress pane is single (a second join while one is
// pending refuses detail-0, DECISION).
//
// NO wall-clock timeout runs server-side (DECISION): the no-reply
// answer is client-composed (the party pane's sub_63c340 arm sends
// answer 2), and an owner whose session dies has the request dropped
// with detail-2 by the lifecycle hooks below.

import (
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/transport"
)

// joinKind separates the two boards' pending requests inside one table.
type joinKind uint8

const (
	joinKindParty joinKind = iota
	joinKindMentor
)

// pendingJoin is one parked join request, keyed by the owner (one per
// owner - see the displacement rule above).
type pendingJoin struct {
	kind       joinKind
	requestID  uint32
	entryID    uint32
	division   string
	ownerName  string
	joinerName string
}

// joinTable holds the pending requests under their own mutex (the
// board's lock stays scoped to listing state).
type joinTable struct {
	mu            sync.Mutex
	nextRequestID uint32
	byOwner       map[string]pendingJoin
}

// newJoinTable returns an empty table; request ids start at 1 (0 stays
// the "never assigned" sentinel in captures).
func newJoinTable() *joinTable {
	return &joinTable{nextRequestID: 1, byOwner: make(map[string]pendingJoin)}
}

// park replaces the owner's pending request with a fresh one, handing
// back the displaced request (displaced=true) so the caller can ack its
// joiner detail-2.
func (t *joinTable) park(kind joinKind, division, ownerName, joinerName string, entryID uint32) (parked pendingJoin, displaced pendingJoin, wasDisplaced bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := ownerKey(division, ownerName)
	displaced, wasDisplaced = t.byOwner[key]
	parked = pendingJoin{
		kind:       kind,
		requestID:  t.nextRequestID,
		entryID:    entryID,
		division:   division,
		ownerName:  ownerName,
		joinerName: joinerName,
	}
	t.nextRequestID++
	t.byOwner[key] = parked
	return parked, displaced, wasDisplaced
}

// take consumes the owner's pending request when the echoed pair and
// kind match it. A miss (never parked, already answered, a reboot, or
// forged echoes) reports false and the caller drops silently.
func (t *joinTable) take(kind joinKind, division, ownerName string, requestID, entryID uint32) (pendingJoin, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := ownerKey(division, ownerName)
	pending, ok := t.byOwner[key]
	if !ok || pending.kind != kind || pending.requestID != requestID || pending.entryID != entryID {
		return pendingJoin{}, false
	}
	delete(t.byOwner, key)
	return pending, true
}

// hasJoiner reports whether a character already has an outgoing pending
// request (the one-per-joiner rule).
func (t *joinTable) hasJoiner(division, joinerName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pending := range t.byOwner {
		if pending.division == division && strings.EqualFold(pending.joinerName, joinerName) {
			return true
		}
	}
	return false
}

// dropByCharacter removes every pending request the character is party
// to, on either side, returning the requests whose JOINERS still await
// an answer (the character was the OWNER - those joiners get the
// detail-2 no-reply ack). Requests where the character was the JOINER
// drop silently - their progress pane died with their session.
func (t *joinTable) dropByCharacter(division, name string) []pendingJoin {
	t.mu.Lock()
	defer t.mu.Unlock()
	var orphaned []pendingJoin
	for key, pending := range t.byOwner {
		if pending.division != division {
			continue
		}
		if strings.EqualFold(pending.ownerName, name) {
			orphaned = append(orphaned, pending)
			delete(t.byOwner, key)
			continue
		}
		if strings.EqualFold(pending.joinerName, name) {
			delete(t.byOwner, key)
		}
	}
	return orphaned
}

// PendingJoinCount reports the number of parked requests (tests and the
// reboot-empty assertions).
func (r *Runtime) PendingJoinCount() int {
	r.joins.mu.Lock()
	defer r.joins.mu.Unlock()
	return len(r.joins.byOwner)
}

// refuseJoin acks one joiner session with the pinned detail arm.
func refuseJoin(s *transport.Session, ackOpcode uint16, detail uint8, joiner, cause string) {
	_ = s.Send(ackOpcode, EncodeJoinAck(detail))
	log.Debugf("match: join from %s refused (detail %d): %s", joiner, detail, cause)
}

/*
================
refuseJoinWithCode

A refusal whose reason the client names: the outer-2 arm with its
category-2 code, so the joiner sees why (level, duplicate, no party).
================
*/
func refuseJoinWithCode(s *transport.Session, ackOpcode uint16, code uint8, joiner, cause string) {
	_ = s.Send(ackOpcode, EncodeJoinError(code))
	log.Debugf("match: join from %s refused (code 0x%02X): %s", joiner, code, cause)
}

// ackDisplaced answers a displaced request's joiner with detail-2 when
// they are still online (their pane sits in no-reply limbo otherwise).
func (r *Runtime) ackDisplaced(displaced pendingJoin) {
	ackOpcode := OpPartyJoinAck
	if displaced.kind == joinKindMentor {
		ackOpcode = OpMentorJoinAck
	}
	if joinerSession, online := r.sessionByName(displaced.division, displaced.joinerName); online {
		_ = joinerSession.Send(ackOpcode, EncodeJoinAck(JoinAckNoReply))
	}
	log.Debugf("match: %s's pending join toward %s displaced - no-reply ack", displaced.joinerName, displaced.ownerName)
}

// handlePartyJoin applies one 0x75BF request: resolve the listing by
// entry id within the division, refuse (detail 0) the owner's own
// listing, a joiner with an outstanding request, a party-seam precheck
// failure (joiner already partied / owner roster full - re-validated at
// commit time under the registry lock), an offline owner (the board
// purges on disconnect, so this is a race guard) and an unwired seam;
// then park the request (displacing an older one with detail 2) and
// notify the owner with the joiner's masked member-info record.
func (r *Runtime) handlePartyJoin(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	entryID, err := DecodeJoinRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	entry, found := r.board.PartyEntryByID(divisionID, entryID)
	if !found {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, "stale party entry id")
		return
	}
	if strings.EqualFold(entry.MasterName, character.Name) {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, "own listing")
		return
	}
	if r.joins.hasJoiner(divisionID, character.Name) {
		refuseJoinWithCode(s, OpPartyJoinAck, JoinErrorDuplicate, character.Name, "a join request is already outstanding")
		return
	}
	if r.PartyJoinPrecheck == nil || r.CommitPartyJoin == nil || r.PartyMemberInfoFor == nil {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, "party seam unwired")
		return
	}
	if refusal := r.PartyJoinPrecheck(divisionID, entry.MasterName, character.Name); refusal != "" {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, refusal)
		return
	}
	if level := characterLevel(character); level < entry.MinLevel || level > entry.MaxLevel {
		refuseJoinWithCode(s, OpPartyJoinAck, JoinErrorLevel, character.Name, "outside listing level range")
		return
	}
	ownerSession, online := r.sessionByName(divisionID, entry.MasterName)
	if !online {
		refuseJoinWithCode(s, OpPartyJoinAck, JoinErrorCantFindParty, character.Name, "listing owner offline")
		return
	}
	// ShardManager 44FAB7 -> 44F7F0 gates applicant job vs purpose;
	// 44FA96 -> 44ED20 also gates existing leader/applicant job compatibility.
	// Both actors satisfying this purpose permits exactly the same job groups.
	owner, _, ownerAvailable := r.sessionIdentity(ownerSession, opcode)
	if !ownerAvailable || !partyPurposeAllowed(activePartyJob(owner), entry.Purpose) || !partyPurposeAllowed(activePartyJob(character), entry.Purpose) {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, "incompatible active job")
		return
	}
	memberInfo, ok := r.PartyMemberInfoFor(divisionID, character.Name)
	if !ok {
		refuseJoin(s, OpPartyJoinAck, JoinAckRefused, character.Name, "joiner record unresolvable")
		return
	}
	parked, displaced, wasDisplaced := r.joins.park(joinKindParty, divisionID, entry.MasterName, character.Name, entryID)
	if wasDisplaced {
		r.ackDisplaced(displaced)
	}
	_ = ownerSession.Send(OpPartyJoinRequest, EncodePartyJoinNotify75BF(parked.requestID, entryID, partyApplicant(character), memberInfo))
	log.Debugf("match: %s asked to join %s's party listing %d (request %d) - owner notified", character.Name, entry.MasterName, entryID, parked.requestID)
}

// handlePartyJoinAnswer applies one 0x30FA owner reply: the sender must
// OWN the pending request the echoed {requestID, entryID} pair names
// (anything else - stale, forged, post-reboot - drops silently; the
// answering pane no longer exists to ack). Accept commits the roster
// change through the internal/game/social/party seam and acks the joiner detail-1;
// a commit refusal (the registry re-validates under its lock - the
// party world may have moved while the prompt was up) and the owner's
// refuse both ack detail-0; the owner's no-reply arm acks detail-2.
func (r *Runtime) handlePartyJoinAnswer(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	answer, err := DecodeJoinAnswer(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	pending, ok := r.joins.take(joinKindParty, divisionID, character.Name, answer.EchoA, answer.EchoB)
	if !ok {
		log.Debugf("match: 0x30FA {%d %d %d} from %s dropped: no matching pending party join", answer.EchoA, answer.EchoB, answer.Answer, character.Name)
		return
	}
	joinerSession, online := r.sessionByName(divisionID, pending.joinerName)
	if answer.Answer != JoinAnswerAccept {
		detail := JoinAckRefused
		if answer.Answer == JoinAnswerNoReply {
			detail = JoinAckNoReply
		}
		if online {
			_ = joinerSession.Send(OpPartyJoinAck, EncodeJoinAck(detail))
		}
		log.Debugf("match: %s answered %d to %s's party join - detail %d", character.Name, answer.Answer, pending.joinerName, detail)
		return
	}
	if !online {
		log.Debugf("match: %s accepted %s's party join but the joiner logged off - nothing commits", character.Name, pending.joinerName)
		return
	}
	// The commit derives the formed party's option bits from the
	// LISTING's type bits when the owner is still partyless - the same
	// party-settings words the registering client folded into them
	// (sub_63bba0 @0x0063bc2c..0x0063bc53), so no second source exists.
	entry, found := r.board.PartyEntryByID(divisionID, pending.entryID)
	if !found || entry.MasterName != pending.ownerName {
		_ = joinerSession.Send(OpPartyJoinAck, EncodeJoinAck(JoinAckRefused))
		return
	}
	joiner, _, available := r.sessionIdentity(joinerSession, OpPartyJoinAnswer)
	if !available || !partyPurposeAllowed(activePartyJob(joiner), entry.Purpose) || !partyPurposeAllowed(activePartyJob(character), entry.Purpose) || characterLevel(joiner) < entry.MinLevel || characterLevel(joiner) > entry.MaxLevel {
		_ = joinerSession.Send(OpPartyJoinAck, EncodeJoinAck(JoinAckRefused))
		return
	}
	optionBits := entry.TypeBits
	if refusal := r.CommitPartyJoin(divisionID, pending.ownerName, pending.joinerName, optionBits); refusal != "" {
		_ = joinerSession.Send(OpPartyJoinAck, EncodeJoinAck(JoinAckRefused))
		log.Debugf("match: accepted party join of %s into %s's party not committed: %s", pending.joinerName, pending.ownerName, refusal)
		return
	}
	_ = joinerSession.Send(OpPartyJoinAck, EncodeJoinAck(JoinAckComplete))
	log.Debugf("match: %s joined %s's party through listing %d", pending.joinerName, pending.ownerName, pending.entryID)
}

// handleMentorJoin is handlePartyJoin's mentor twin: the notify carries
// the joiner's level pair + model ref + name (the msgbox kind-0xe
// consumer's pinned reads) and the commit runs through the internal/game/social/mentor
// seam.
func (r *Runtime) handleMentorJoin(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	entryID, err := DecodeJoinRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	entry, found := r.board.MentorEntryByID(divisionID, entryID)
	if !found {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, "stale mentor entry id")
		return
	}
	if strings.EqualFold(entry.Requester, character.Name) {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, "own listing")
		return
	}
	if r.joins.hasJoiner(divisionID, character.Name) {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, "a join request is already outstanding")
		return
	}
	if r.MentorJoinPrecheck == nil || r.CommitMentorJoin == nil {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, "mentor seam unwired")
		return
	}
	if refusal := r.MentorJoinPrecheck(divisionID, entry.Requester, character.Name); refusal != "" {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, refusal)
		return
	}
	ownerSession, online := r.sessionByName(divisionID, entry.Requester)
	if !online {
		refuseJoin(s, OpMentorJoinAck, JoinAckRefused, character.Name, "listing owner offline")
		return
	}
	parked, displaced, wasDisplaced := r.joins.park(joinKindMentor, divisionID, entry.Requester, character.Name, entryID)
	if wasDisplaced {
		r.ackDisplaced(displaced)
	}
	_ = ownerSession.Send(OpMentorJoinRequest, EncodeMentorJoinNotify7592(
		parked.requestID,
		entryID,
		characterLevel(character),
		r.deps.CharacterModelRef(character),
		character.Name,
	))
	log.Debugf("match: %s asked to join %s's mentor listing %d (request %d) - owner notified", character.Name, entry.Requester, entryID, parked.requestID)
}

// handleMentorJoinAnswer is handlePartyJoinAnswer's mentor twin on
// 0x35D5 / 0xB592, committing through the internal/game/social/mentor seam (which gets
// the JOINER's live session for its 0x3AC5 camp seed).
func (r *Runtime) handleMentorJoinAnswer(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	answer, err := DecodeJoinAnswer(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	pending, ok := r.joins.take(joinKindMentor, divisionID, character.Name, answer.EchoA, answer.EchoB)
	if !ok {
		log.Debugf("match: 0x35D5 {%d %d %d} from %s dropped: no matching pending mentor join", answer.EchoA, answer.EchoB, answer.Answer, character.Name)
		return
	}
	joinerSession, online := r.sessionByName(divisionID, pending.joinerName)
	if answer.Answer != JoinAnswerAccept {
		detail := JoinAckRefused
		if answer.Answer == JoinAnswerNoReply {
			detail = JoinAckNoReply
		}
		if online {
			_ = joinerSession.Send(OpMentorJoinAck, EncodeJoinAck(detail))
		}
		log.Debugf("match: %s answered %d to %s's mentor join - detail %d", character.Name, answer.Answer, pending.joinerName, detail)
		return
	}
	if !online {
		log.Debugf("match: %s accepted %s's mentor join but the joiner logged off - nothing commits", character.Name, pending.joinerName)
		return
	}
	if refusal := r.CommitMentorJoin(joinerSession, divisionID, pending.ownerName, pending.joinerName); refusal != "" {
		_ = joinerSession.Send(OpMentorJoinAck, EncodeJoinAck(JoinAckRefused))
		log.Debugf("match: accepted mentor join of %s under %s not committed: %s", pending.joinerName, pending.ownerName, refusal)
		return
	}
	_ = joinerSession.Send(OpMentorJoinAck, EncodeJoinAck(JoinAckComplete))
	log.Debugf("match: %s joined %s's academy through listing %d", pending.joinerName, pending.ownerName, pending.entryID)
}

// dropJoinRequestsFor runs the session-boundary leg for one character:
// requests they OWNED orphan their joiners (detail-2 no-reply - the
// pane that could answer died), requests they SENT drop silently (the
// progress pane died with them). Shared by SessionClosed and the
// WorldBound stale-state purge.
func (r *Runtime) dropJoinRequestsFor(divisionID, name string) {
	for _, orphan := range r.joins.dropByCharacter(divisionID, name) {
		r.ackDisplaced(orphan)
	}
}
