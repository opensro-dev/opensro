/*
===========================================================================

friendrequest.go - the friend request: ask the target, then add on consent

Native v1.150 adds a friend only with the other player's consent:

	requester  C->S 0x7164 {name}                    (sub_704590)
	target     S->C 0x7164 {u32 jid, u32 serial, name} -> confirm box 0x23
	                                                   (CPSMission 760C90)
	target     C->S 0x3012 {u32 jid, u32 serial, u8 accept}
	                                                   (OnMsgBoxResult, 701A50)
	requester  S->C 0xB164 {u8 1} or {u8 2, u8 code[, ...]} (760AF0)

The checks and their codes are the reference server's: the game server's
friend add and the shard manager's request job, whose error
words' low bytes are the v1.150 client's 0x13 notice codes. A request
waits 30 s for its answer (the ShardManager job's 30.0f timeout); a
timeout or a party leaving ends it on both sides with the client's
"cancelled" form (0xB164 {2, 0x0B, 2, name}: the requester sees
UIIT_MSG_FRIEND_SYSCANCEL, the target's prompt is retired).

One owner keeps the requests in flight. They are not persisted: a restart
drops every unanswered request, as the native job table would.

===========================================================================
*/
package community

import (
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// OpFriendInviteAnswer is the requester's result (S->C 0xB164).
const OpFriendInviteAnswer uint16 = 0xB164

// OpFriendDeleteAnswer is the delete result (S->C 0xB5DB, 75CE90).
const OpFriendDeleteAnswer uint16 = 0xB5DB

// FriendConsentLength is the 0x3012 consent body: u32 jid, u32 serial,
// u8 accept. The port's own client also sends 0x3012, empty, as its
// game-ready signal; the body length tells the two apart
// (enterworld.RegisterGameReady).
const FriendConsentLength = enterworld.ConsentAnswerLength

// FriendAnswerWindow is the ShardManager request job's 30.0f timeout
// (natively).
const FriendAnswerWindow = 30 * time.Second

// FriendNameMaxLength is the native friend add's name bound (1..0x20).
const FriendNameMaxLength = 0x20

// 0xB164 result bytes (760AF0).
const (
	friendResultOK    uint8 = 1
	friendResultError uint8 = 2
)

// 0xB164 error codes: v1.188's 0x64xx words' low bytes, which the v1.150
// client shows as notice 0x13/code (CGInterface_ShowSystemNotification).
const (
	FriendErrInvalidName    uint8 = 0x02 // name length outside 1..32
	FriendErrSelf           uint8 = 0x03 // own name
	FriendErrListFull       uint8 = 0x08 // own list at 20
	FriendErrAlreadyFriend  uint8 = 0x09 // already listed
	FriendErrConsent        uint8 = 0x0B // carries a consent sub-reason
	FriendErrUnknownUser    uint8 = 0x0D // target absent or not in game
	FriendErrInProgress     uint8 = 0x0E // a request already waits (see Ask)
	FriendErrTargetListFull uint8 = 0x0F // target list at 20
)

// Sub-reasons of FriendErrConsent (760AF0).
const (
	friendConsentRefused   uint8 = 0
	friendConsentCancelled uint8 = 2
)

/*
================
EncodeFriendInvitePrompt7164

The target's prompt: u32 requester jid, u32 serial, sized requester name.
The client echoes both u32s back in its 0x3012 answer.
================
*/
func EncodeFriendInvitePrompt7164(requesterJID, serial uint32, requesterName string) []byte {
	writer := wire.NewWriter(10 + len(requesterName))
	writer.U32(requesterJID)
	writer.U32(serial)
	writeSizedString(writer, requesterName)
	return writer.Payload()
}

/*
================
EncodeFriendInviteOKB164
================
*/
func EncodeFriendInviteOKB164() []byte {
	return []byte{friendResultOK}
}

/*
================
EncodeFriendInviteErrorB164
================
*/
func EncodeFriendInviteErrorB164(code uint8) []byte {
	return []byte{friendResultError, code}
}

/*
================
EncodeFriendConsentEndB164

The consent form of the error: refused (with the target's name) or
cancelled (with the other party's name).
================
*/
func EncodeFriendConsentEndB164(sub uint8, name string) []byte {
	writer := wire.NewWriter(5 + len(name))
	writer.U8(friendResultError)
	writer.U8(FriendErrConsent)
	writer.U8(sub)
	writeSizedString(writer, name)
	return writer.Payload()
}

/*
================
EncodeFriendDeleteOKB5DB
================
*/
func EncodeFriendDeleteOKB5DB(jid uint32) []byte {
	return wire.NewWriter(5).U8(friendResultOK).U32(jid).Payload()
}

/*
================
EncodeFriendDeleteErrorB5DB
================
*/
func EncodeFriendDeleteErrorB5DB(code uint8) []byte {
	return []byte{friendResultError, code}
}

/*
================
DecodeFriendConsent3012

The target's answer: u32 jid, u32 serial, u8 accept (1) or refuse (0).
================
*/
func DecodeFriendConsent3012(payload []byte) (jid, serial uint32, accept bool, err error) {
	reader := wire.NewReader(payload)
	if jid, err = reader.U32(); err != nil {
		return 0, 0, false, err
	}
	if serial, err = reader.U32(); err != nil {
		return 0, 0, false, err
	}
	answer, err := reader.U8()
	if err != nil {
		return 0, 0, false, err
	}
	if err := reader.Done(); err != nil {
		return 0, 0, false, err
	}
	if answer > 1 {
		return 0, 0, false, fmt.Errorf("community: friend consent answer %d is not 0 or 1", answer)
	}
	return jid, serial, answer == 1, nil
}

/*
================
CheckFriendAdd

The target of a friend request and 0, or the native refusal code, in the
native order: the game server's friend add (name, self, listed, own cap), then the
shard manager's request job (target absent or offline, target cap, listed from
the target's side).

The native friend add also answers 3 for a name the forbidden-name lookup matches (the
forbidden-name table). It is skipped here: a request resolves only to an
existing character, whose name already passed character creation, so a
rejected name can only ever answer UNKNOWNUSER, as an unknown name does.
================
*/
func CheckFriendAdd(deps Dependencies, divisionID string, actor *enterworld.Character, name string, online func(string) bool) (*enterworld.Character, uint8) {
	if len(name) < 1 || len(name) > FriendNameMaxLength {
		return nil, FriendErrInvalidName
	}
	if strings.EqualFold(name, actor.Name) {
		return nil, FriendErrSelf
	}
	target := findCharacterByName(deps, divisionID, name)
	var code uint8
	deps.Read(divisionID, func() {
		switch {
		case target != nil && friendListed(actor, target.ID):
			code = FriendErrAlreadyFriend
		case len(actor.Friends) >= FriendMaxCount:
			code = FriendErrListFull
		case target == nil || target.DeletePending || online == nil || !online(target.Name):
			code = FriendErrUnknownUser
		case len(target.Friends) >= FriendMaxCount:
			code = FriendErrTargetListFull
		case friendListed(target, actor.ID):
			code = FriendErrAlreadyFriend
		}
	})
	if code != 0 {
		return nil, code
	}
	return target, 0
}

/*
================
friendRequest
================
*/
type friendRequest struct {
	divisionID    string
	serial        uint32
	requesterID   int64
	requesterName string
	targetID      int64
	targetName    string
	stop          func() bool
}

/*
================
FriendRequests

The friend requests in flight, one owner for the lane. After schedules
the answer window (time.AfterFunc unless a test replaces it).
================
*/
type FriendRequests struct {
	deps     Dependencies
	presence Presence

	mu      sync.Mutex
	pending map[uint32]*friendRequest
	serial  uint32

	After func(time.Duration, func()) func() bool
}

/*
================
NewFriendRequests
================
*/
func NewFriendRequests(deps Dependencies, presence Presence) *FriendRequests {
	return &FriendRequests{
		deps:     deps,
		presence: presence,
		pending:  map[uint32]*friendRequest{},
		After: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
	}
}

/*
================
FriendRequests.busy

Whether either character takes part in a waiting request. The caller
holds mu.
================
*/
func (r *FriendRequests) busy(divisionID string, ids ...int64) bool {
	for _, request := range r.pending {
		if request.divisionID != divisionID {
			continue
		}
		for _, id := range ids {
			if request.requesterID == id || request.targetID == id {
				return true
			}
		}
	}
	return false
}

/*
================
FriendRequests.Ask

Handles one C->S 0x7164. A refusal answers the requester at once; an
accepted request prompts the target and waits FriendAnswerWindow.

Not found yet in v1.188 (the request job's class was opaque): which code
a second request gets while one waits. FriendErrInProgress (0x0E,
UIIT_MSG_FRIEND_SYSPROGRESS, "a request is in progress") is the client's
own text for it, and only one prompt per player can be on screen.
================
*/
func (r *FriendRequests) Ask(s *transport.Session, divisionID string, actor *enterworld.Character, payload []byte) {
	name, err := DecodeFriendAddRequest(payload)
	if err != nil {
		log.Debugf("community: malformed 0x7164 from %s: %v", actor.Name, err)
		return
	}
	online := func(n string) bool { return presenceOnline(r.presence, divisionID, n) }
	target, code := CheckFriendAdd(r.deps, divisionID, actor, name, online)
	if code != 0 {
		_ = s.Send(OpFriendInviteAnswer, EncodeFriendInviteErrorB164(code))
		return
	}
	peer, ok := presenceSession(r.presence, divisionID, target.Name)
	if !ok {
		_ = s.Send(OpFriendInviteAnswer, EncodeFriendInviteErrorB164(FriendErrUnknownUser))
		return
	}
	r.mu.Lock()
	if r.busy(divisionID, actor.ID, target.ID) {
		r.mu.Unlock()
		_ = s.Send(OpFriendInviteAnswer, EncodeFriendInviteErrorB164(FriendErrInProgress))
		return
	}
	r.serial++
	if r.serial == 0 {
		r.serial = 1
	}
	request := &friendRequest{
		divisionID:    divisionID,
		serial:        r.serial,
		requesterID:   actor.ID,
		requesterName: actor.Name,
		targetID:      target.ID,
		targetName:    target.Name,
	}
	r.pending[request.serial] = request
	serial := request.serial
	request.stop = r.After(FriendAnswerWindow, func() { r.expire(serial) })
	r.mu.Unlock()
	_ = peer.Send(OpFriendAddRequest, EncodeFriendInvitePrompt7164(FriendJID(actor.ID), serial, actor.Name))
}

/*
================
FriendRequests.take

Removes and returns a waiting request; nil when it already ended.
================
*/
func (r *FriendRequests) take(serial uint32) *friendRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	request := r.pending[serial]
	if request == nil {
		return nil
	}
	delete(r.pending, serial)
	if request.stop != nil {
		request.stop()
	}
	return request
}

/*
================
FriendRequests.cancel

Ends a request on both sides with the client's cancelled form.
================
*/
func (r *FriendRequests) cancel(request *friendRequest) {
	if peer, ok := presenceSession(r.presence, request.divisionID, request.requesterName); ok {
		_ = peer.Send(OpFriendInviteAnswer, EncodeFriendConsentEndB164(friendConsentCancelled, request.targetName))
	}
	if peer, ok := presenceSession(r.presence, request.divisionID, request.targetName); ok {
		_ = peer.Send(OpFriendInviteAnswer, EncodeFriendConsentEndB164(friendConsentCancelled, request.requesterName))
	}
}

/*
================
FriendRequests.expire
================
*/
func (r *FriendRequests) expire(serial uint32) {
	if request := r.take(serial); request != nil {
		r.cancel(request)
	}
}

/*
================
FriendRequests.Answer

Handles the target's 0x3012 consent. Only the prompted character may
answer, with the jid and serial it was given. A refusal tells the
requester who refused; a consent appends the mutual edges through the
store door, which checks both lists again.
================
*/
func (r *FriendRequests) Answer(s *transport.Session, divisionID string, actor *enterworld.Character, payload []byte) {
	jid, serial, accept, err := DecodeFriendConsent3012(payload)
	if err != nil {
		log.Debugf("community: malformed friend consent from %s: %v", actor.Name, err)
		return
	}
	r.mu.Lock()
	request := r.pending[serial]
	matches := request != nil && request.divisionID == divisionID && request.targetID == actor.ID &&
		FriendJID(request.requesterID) == jid
	r.mu.Unlock()
	if !matches {
		log.Debugf("community: friend consent from %s matches no waiting request", actor.Name)
		return
	}
	if r.take(serial) == nil {
		return
	}
	requesterSession, requesterOnline := presenceSession(r.presence, divisionID, request.requesterName)
	if !accept {
		if requesterOnline {
			_ = requesterSession.Send(OpFriendInviteAnswer, EncodeFriendConsentEndB164(friendConsentRefused, request.targetName))
		}
		return
	}
	requester := findCharacterByID(r.deps, divisionID, request.requesterID)
	if requester == nil || !requesterOnline {
		_ = s.Send(OpFriendInviteAnswer, EncodeFriendConsentEndB164(friendConsentCancelled, request.requesterName))
		return
	}
	targetEdge, actorEdge, applied := AppendMutualFriendEdges(r.deps, requester, actor)
	if !applied {
		// The lists changed while the prompt was open: name what stopped it.
		_, code := CheckFriendAdd(r.deps, divisionID, requester, actor.Name, func(string) bool { return true })
		if code == 0 {
			code = FriendErrInProgress
		}
		_ = requesterSession.Send(OpFriendInviteAnswer, EncodeFriendInviteErrorB164(code))
		return
	}
	_ = requesterSession.Send(OpFriendLetterEventPush, EncodeFriendEventAdded3F9A(FriendJID(targetEdge.ID), targetEdge.Name, targetEdge.ModelRefID))
	_ = requesterSession.Send(OpFriendInviteAnswer, EncodeFriendInviteOKB164())
	_ = s.Send(OpFriendLetterEventPush, EncodeFriendEventAdded3F9A(FriendJID(actorEdge.ID), actorEdge.Name, actorEdge.ModelRefID))
	log.Debugf("community: %s and %s are now friends", request.requesterName, request.targetName)
}

/*
================
FriendRequests.SessionClosed

A party leaving ends its waiting requests; the other side is told. A
session that lost its bind to a newer one leaves nothing: the character
is still in the game (FriendSessionClosed's rule).
================
*/
func (r *FriendRequests) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := presenceSession(r.presence, divisionID, character.Name); live && winner != s {
		return
	}
	r.mu.Lock()
	var ended []uint32
	for serial, request := range r.pending {
		if request.divisionID == divisionID && (request.requesterID == character.ID || request.targetID == character.ID) {
			ended = append(ended, serial)
		}
	}
	r.mu.Unlock()
	for _, serial := range ended {
		if request := r.take(serial); request != nil {
			r.cancel(request)
		}
	}
}

/*
================
FriendRequests.PendingCount
================
*/
func (r *FriendRequests) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}
