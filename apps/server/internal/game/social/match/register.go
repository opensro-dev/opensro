package match

import (
	"opensro.online/server/internal/domain"
	"strings"
	"unicode/utf16"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// Runtime owns the match lane's board and the deps it resolves session
// identity through.
type Runtime struct {
	deps  Dependencies
	board *Board
	joins *joinTable
	// presence resolves live owners and rebind winners without a parallel
	// session map.
	presence Presence

	// MemberCountFor derives a party-match listing's live member count
	// from the party registry (this lane must not import internal/game/social/party -
	// party already imports community and the seam keeps the dependency
	// one-way). Assigned by server.go; nil, and any answer below 1,
	// falls back to the client's own no-active-party fallback of 1.
	MemberCountFor func(divisionID, name string) int
	// Party registry owns leadership and options; a listing cannot override them.
	PartyListingAuthority func(divisionID, name string) (options uint8, partied bool, leader bool)

	// The party-join seams (the MemberCountFor posture: func fields
	// assigned by wiring.go so internal/game/social/party stays this lane's dependency
	// through ONE hookup point, never an import). internal/game/social/party owns
	// parties - this lane never mutates roster state itself.
	//
	// PartyJoinPrecheck answers a request-time refusal reason ("" = ok):
	// joiner already partied, owner's roster full, owner partied but not
	// its leader. The commit RE-validates under the registry lock.
	PartyJoinPrecheck func(divisionID, ownerName, joinerName string) string
	// CommitPartyJoin makes the accepted roster change through the party
	// runtime: join when the owner is partied, FORM (leader = owner,
	// optionBits = the listing's type bits) when partyless. Returns the
	// refusal reason ("" = committed; the party lane's own pinned frames
	// ride from inside the commit).
	CommitPartyJoin func(divisionID, ownerName, joinerName string, optionBits uint8) string
	// PartyMemberInfoFor renders the joiner's masked member-info record
	// (the sub_75db30 wire shape) through internal/game/social/party's ONE encoder - the
	// 0x75BF owner notify's tail.
	PartyMemberInfoFor func(divisionID, name string) ([]byte, bool)

	// The mentor-join seams (internal/game/social/mentor owns the mentor/TC
	// relationship; same one-way posture - see wiring.go).
	//
	// MentorJoinPrecheck answers a request-time refusal reason ("" =
	// ok): joiner already camped, over the student band, owner not a
	// recognizable master. Re-validated at commit time.
	MentorJoinPrecheck func(divisionID, ownerName, joinerName string) string
	// CommitMentorJoin commits the camp membership through internal/game/social/mentor's
	// atomic doors (creating the camp with its first member when the
	// master has none - that lane's documented camp-creation DECISION)
	// and fans its own 0x3AC5 frames; the joiner's live session receives
	// the status-10 sub-1 seed.
	CommitMentorJoin func(joinerSession *transport.Session, divisionID, ownerName, joinerName string) string
}

// NewRuntime builds the lane over a fresh in-memory board and retains the
// process-owned deps pointer shared by every lane.
func NewRuntime(deps Dependencies, presence Presence) *Runtime {
	return &Runtime{
		deps:     deps,
		presence: presence,
		board:    NewBoard(),
		joins:    newJoinTable(),
	}
}

// Board exposes the registry for tests.
func (r *Runtime) Board() *Board {
	return r.board
}

func (r *Runtime) sessionByName(divisionID, characterName string) (*transport.Session, bool) {
	if r.presence == nil {
		return nil, false
	}
	return r.presence.SessionByName(divisionID, characterName)
}

// Register wires the twelve match C->S opcodes onto the hub: the eight
// register/modify/delete/page legs plus the JOIN handshake (0x75BF /
// 0x7592 requests and their 0x30FA / 0x35D5 owner answers - join.go;
// both opcode pairs are PINNED). Each registers exactly once and none is registered by any
// other lane. Refusals of the register/modify/delete legs stay SILENT -
// the flag-2 error-code tables (party category 2, mentor 0x1D) are
// unpinned vs retail and this lane never invents bytes; JOIN refusals
// answer the PINNED 0xB5BF/0xB592 outer-1 detail arms instead (join.go
// documents why silence is wrong there).
func (r *Runtime) Register(hub *transport.Hub) {
	hub.Handle(OpPartyRegisterRequest, r.handlePartyRegister)
	hub.Handle(OpPartyModifyRequest, r.handlePartyModify)
	hub.Handle(OpPartyDeleteRequest, r.handlePartyDelete)
	hub.Handle(OpPartyPageRequest, r.handlePartyPage)
	hub.Handle(OpMentorRegisterRequest, r.handleMentorRegister)
	hub.Handle(OpMentorModifyRequest, r.handleMentorModify)
	hub.Handle(OpMentorDeleteRequest, r.handleMentorDelete)
	hub.Handle(OpMentorPageRequest, r.handleMentorPage)
	hub.Handle(OpPartyJoinRequest, r.handlePartyJoin)
	hub.Handle(OpPartyJoinAnswer, r.handlePartyJoinAnswer)
	hub.Handle(OpMentorJoinRequest, r.handleMentorJoin)
	hub.Handle(OpMentorJoinAnswer, r.handleMentorJoinAnswer)
}

// ownerKey is the board's owner identity: the same division+":"+lowered
// name shape the hub's exclusive bind uses.
func ownerKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}

// sessionIdentity resolves the bound character or reports the silent
// discard (unbound sessions never reach board state).
func (r *Runtime) sessionIdentity(s *transport.Session, opcode uint16) (*enterworld.Character, string, bool) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		log.Debugf("match: 0x%04X from unbound session %d discarded", opcode, s.ID)
		return nil, "", false
	}
	var snapshot *enterworld.Character
	r.deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	if snapshot == nil || snapshot.DeletePending {
		log.Debugf("match: 0x%04X from unavailable character on session %d discarded", opcode, s.ID)
		return nil, "", false
	}
	return snapshot, divisionID, true
}

// characterLevel reads the persisted level with the store's >=1 floor.
func characterLevel(c *enterworld.Character) uint8 {
	if c == nil || c.Level == nil || *c.Level < 1 {
		return 1
	}
	if *c.Level > 0xFF {
		return 0xFF
	}
	return uint8(*c.Level)
}

// partyEntryFromRequest builds the registration snapshot: the request's
// four option bytes + title, the SERVER-derived master identity (name,
// native country byte), and the member count off the party registry seam
// (1 when the owner is partyless - the client's own ack writer uses the
// same no-active-party fallback).
func (r *Runtime) partyEntryFromRequest(divisionID string, character *enterworld.Character, request PartyMatchRequest) PartyEntry {
	return PartyEntry{
		PartyNumber: request.PartyNumber,
		MasterName:  character.Name,
		RaceByte:    uint8(enterworld.NativeCountryByte9C(character)),
		MemberCount: r.partyMemberCount(divisionID, character.Name),
		TypeBits:    request.TypeBits,
		Purpose:     request.Purpose,
		MinLevel:    request.MinLevel,
		MaxLevel:    request.MaxLevel,
		Title:       request.Title,
	}
}

// partyMemberCount answers the MemberCount byte for one owner: the
// MemberCountFor seam's live count when the owner is in a party, 1
// otherwise. The wire field is a u8 and the party roster caps at 8, so
// the clamp never fires in practice - it only keeps a misbehaving seam
// from wrapping the byte.
func (r *Runtime) partyMemberCount(divisionID, name string) uint8 {
	if r.MemberCountFor == nil {
		return 1
	}
	count := r.MemberCountFor(divisionID, name)
	if count < 1 {
		return 1
	}
	if count > 0xFF {
		return 0xFF
	}
	return uint8(count)
}

// mentorEntryFromRequest builds the mentor registration: the request's
// kind + detail + dword04, the SERVER-derived requester identity (name,
// model ref for the client-side race resolve, level into the window's
// level pair), and zeroed camp/honor scalars (no training-camp state).
func (r *Runtime) mentorEntryFromRequest(character *enterworld.Character, request MentorMatchRequest) MentorEntry {
	level := characterLevel(character)
	return MentorEntry{
		Kind:      request.Kind,
		Detail:    request.Detail,
		Dword04:   request.Dword04,
		LevelAlt:  level,
		Level:     level,
		RefObjID:  r.deps.CharacterModelRef(character),
		Requester: character.Name,
	}
}

// handlePartyRegister answers 0x76FF with the 0xB6FF success ack. A
// duplicate registration returns native category-2 unknown error (modify is 0x73DC).
func (r *Runtime) handlePartyRegister(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	request, err := DecodePartyMatchRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	request, refusal := r.preparePartyRegistration(divisionID, character, request)
	if refusal != 0 {
		_ = s.Send(opcode+0x4000, []byte{2, refusal})
		return
	}
	entry, ok := r.board.RegisterParty(divisionID, ownerKey(divisionID, character.Name), r.partyEntryFromRequest(divisionID, character, request))
	if !ok {
		log.Debugf("match: party register from %s refused (already registered)", character.Name)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	_ = s.Send(OpPartyRegisterAck, EncodePartyRegAck(entry))
	log.Debugf("match: party entry %d registered by %s", entry.EntryID, character.Name)
}

// handlePartyModify answers 0x73DC with the 0xB3DC success ack. A
// modify without a live registration returns a native error.
func (r *Runtime) handlePartyModify(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	request, err := DecodePartyMatchRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	request, refusal := r.preparePartyRegistration(divisionID, character, request)
	if refusal != 0 {
		_ = s.Send(opcode+0x4000, []byte{2, refusal})
		return
	}
	entry, ok := r.board.ModifyParty(ownerKey(divisionID, character.Name), r.partyEntryFromRequest(divisionID, character, request))
	if !ok {
		log.Debugf("match: party modify from %s refused (no registration)", character.Name)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	_ = s.Send(OpPartyModifyAck, EncodePartyRegAck(entry))
	log.Debugf("match: party entry %d modified by %s", entry.EntryID, character.Name)
}

// handlePartyDelete answers 0x7535 with the 0xB535 success ack. Foreign
// or unknown ids return a native error - only the owner deletes their row.
func (r *Runtime) handlePartyDelete(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	entryID, err := DecodeDeleteRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	if !r.board.DeleteParty(ownerKey(divisionID, character.Name), entryID) {
		log.Debugf("match: party delete %d from %s refused (not the owner's entry)", entryID, character.Name)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	_ = s.Send(OpPartyDeleteAck, EncodeDeleteAck(entryID))
	log.Debugf("match: party entry %d deleted by %s", entryID, character.Name)
}

// handlePartyPage answers 0x7588 with the 0xB588 listing page.
func (r *Runtime) handlePartyPage(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	page, err := DecodePageRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		_ = s.Send(opcode+0x4000, []byte{2, 2})
		return
	}
	curPage, pageCount, rows := r.board.PartyPage(divisionID, ownerKey(divisionID, character.Name), page)
	_ = s.Send(OpPartyListingPage, EncodePartyListingB588(curPage, pageCount, rows))
}

// handleMentorRegister answers 0x755D with the 0xB55D success ack.
func (r *Runtime) handleMentorRegister(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	request, err := DecodeMentorMatchRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	entry, ok := r.board.RegisterMentor(divisionID, ownerKey(divisionID, character.Name), r.mentorEntryFromRequest(character, request))
	if !ok {
		log.Debugf("match: mentor register from %s refused silently (already registered; flag-2 codes unpinned)", character.Name)
		return
	}
	_ = s.Send(OpMentorRegisterAck, EncodeMentorRegisterAckB55D(entry))
	log.Debugf("match: mentor entry %d registered by %s", entry.EntryID, character.Name)
}

// handleMentorModify answers 0x713E with the 0xB13E success ack.
func (r *Runtime) handleMentorModify(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	request, err := DecodeMentorMatchRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	entry, ok := r.board.ModifyMentor(ownerKey(divisionID, character.Name), r.mentorEntryFromRequest(character, request))
	if !ok {
		log.Debugf("match: mentor modify from %s refused silently (no registration; flag-2 codes unpinned)", character.Name)
		return
	}
	_ = s.Send(OpMentorModifyAck, EncodeMentorModifyAckB13E(entry))
	log.Debugf("match: mentor entry %d modified by %s", entry.EntryID, character.Name)
}

// handleMentorDelete answers 0x770B with the 0xB70B success ack.
func (r *Runtime) handleMentorDelete(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	entryID, err := DecodeDeleteRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	if !r.board.DeleteMentor(ownerKey(divisionID, character.Name), entryID) {
		log.Debugf("match: mentor delete %d from %s refused silently (not the owner's entry)", entryID, character.Name)
		return
	}
	_ = s.Send(OpMentorDeleteAck, EncodeDeleteAck(entryID))
	log.Debugf("match: mentor entry %d deleted by %s", entryID, character.Name)
}

// handleMentorPage answers 0x7701 with the 0xB701 listing page.
func (r *Runtime) handleMentorPage(s *transport.Session, opcode uint16, payload []byte) {
	character, divisionID, ok := r.sessionIdentity(s, opcode)
	if !ok {
		return
	}
	page, err := DecodePageRequest(payload)
	if err != nil {
		log.Debugf("match: 0x%04X malformed from %s: %v", opcode, character.Name, err)
		return
	}
	curPage, pageCount, rows := r.board.MentorPage(divisionID, ownerKey(divisionID, character.Name), page)
	_ = s.Send(OpMentorListingPage, EncodeMentorListingB701(curPage, pageCount, rows))
}

// WorldBound is the enter-world hook, called from the server's
// OnWorldBound tail on the winner path: a character whose rows still sit
// on a board (their previous session was replaced without closing first)
// has them PURGED - the fresh client holds no registration snapshot, so
// a surviving row would be a ghost listing other players can act on. The
// party lane's posture exactly: a session boundary always drops the
// state, never reseeds it (reseeding would race the bootstrap frame
// order; in-memory match state does not survive the player's transport).
func (r *Runtime) WorldBound(divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	if r.board.PurgeOwner(ownerKey(divisionID, character.Name)) {
		log.Debugf("match: %s re-entered the world; stale board rows purged", character.Name)
	}
	// Pending join requests are session state on BOTH ends: the fresh
	// client holds neither the owner's request pane nor the joiner's
	// progress pane, so requests they owned orphan their joiners with
	// the detail-2 no-reply ack and requests they sent drop silently
	// (join.go documents the posture).
	r.dropJoinRequestsFor(divisionID, character.Name)
}

// SessionClosed is the disconnect hook, called from the hub's
// OnSessionClose: resolve the closing session's bound character (an
// evicted loser had its name key cleared at bind time and resolves to
// nothing), skip when ANOTHER session already holds the bind key (the
// rebind winner is live - the character never went offline, and the
// winner's rows must survive its predecessor's close), then purge the
// owner's rows from both boards.
func (r *Runtime) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.sessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	if r.board.PurgeOwner(ownerKey(divisionID, character.Name)) {
		log.Debugf("match: %s disconnected; board rows purged", character.Name)
	}
	// The WorldBound rationale: a request pane / progress pane died
	// with this transport, so owned requests orphan their joiners with
	// detail-2 and sent requests drop silently.
	r.dropJoinRequestsFor(divisionID, character.Name)
}

// Party-matching refusal codes (category 2), from the server register
// precheck sub_514170 and its purpose test sub_5BF240 (0x2C0A, 0x2C1D,
// 0x2C23); the v1.150 client shows them as UIIT_MSG_PARTYERR_* /
// UIIT_MSG_PARTYMATCH_RECORD_ERROR_* through the 0x200 notice table.
const (
	partyMatchErrUnknown        uint8 = 0x02
	partyMatchErrCreatorLevel   uint8 = 0x0A
	partyMatchErrNotPartyLeader uint8 = 0x1D
	partyMatchErrPurpose        uint8 = 0x23
)

// minPartyCreatorLevel is the level a partyless registrant needs: sub_514170
// refuses level < 5 with 0x2C0A when the character has no party.
const minPartyCreatorLevel = 5

/*
================
preparePartyRegistration

Retail 63BBA0/63C010 validate the form; authority independently enforces
it. A party member who is not the master is refused (0x1D), a partyless
registrant below level 5 too (0x0A); a party's listing takes the party's
options. Returns the refusal code, 0 when admitted.
================
*/
func (r *Runtime) preparePartyRegistration(division string, character *domain.Character, request PartyMatchRequest) (PartyMatchRequest, uint8) {
	titleLength := len(utf16.Encode([]rune(request.Title)))
	if request.TypeBits&^7 != 0 || request.MinLevel < 1 || request.MaxLevel > 90 || request.MinLevel > request.MaxLevel || titleLength == 0 || titleLength > 50 {
		return request, partyMatchErrUnknown
	}
	partied, leader, options := false, false, uint8(0)
	if r.PartyListingAuthority != nil {
		options, partied, leader = r.PartyListingAuthority(division, character.Name)
	}
	if partied && !leader {
		return request, partyMatchErrNotPartyLeader
	}
	if !partied && characterLevel(character) < minPartyCreatorLevel {
		return request, partyMatchErrCreatorLevel
	}
	if !partyPurposeAllowed(activePartyJob(character), request.Purpose) {
		return request, partyMatchErrPurpose
	}
	if partied {
		request.TypeBits = options
	}
	return request, 0
}
