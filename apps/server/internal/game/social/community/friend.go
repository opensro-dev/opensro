package community

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// FriendMaxCount is the client panel's row cap: the literal 0x14 in the
// client's ": %d / %d" count write over the +0x7c4 pair list. The
// server enforces it on BOTH sides of the mutual edge so no persisted
// roster can exceed what the panel displays.
const FriendMaxCount = enterworld.FriendMaxCount

// 0x3F9A event-type bytes the client's
// CPSMission_OnFriendConnectNotice0x3F9A jump table dispatches
// (cases 5/6/7 are table holes and anything outside 2..9 asserts - only
// these three are the friend lane's to compose; 8/9 belong to letters).
const (
	FriendEventAdded   uint8 = 2
	FriendEventDeleted uint8 = 3
	FriendEventState   uint8 = 4
)

// Friend state bytes (the client's roster read / the client case-4
// state write:
// 0 online flips the gil_contact_on art, 1 offline flips it off).
const (
	FriendStateOnline  uint8 = 0
	FriendStateOffline uint8 = 1
)

// DecodeFriendAddRequest strict-decodes the client NetClient_Send7164
// body
// {u16 len, ANSI name}: a non-empty name and no trailing bytes (the
// client's 0x8075 receiver CIFFriendPanel_OnAddNameBoxResult already
// drops empty input, so an
// empty wire name is malformed, not a refusal).
func DecodeFriendAddRequest(payload []byte) (string, error) {
	reader := wire.NewReader(payload)
	nameLen, err := reader.U16()
	if err != nil {
		return "", err
	}
	nameBytes, err := reader.Bytes(int(nameLen))
	if err != nil {
		return "", err
	}
	if err := reader.Done(); err != nil {
		return "", err
	}
	if nameLen == 0 {
		return "", fmt.Errorf("community: friend-add name is empty")
	}
	return string(nameBytes), nil
}

// DecodeFriendDeleteRequest strict-decodes the client NetClient_Send75DB
// body
// {u32 friendJid}.
func DecodeFriendDeleteRequest(payload []byte) (uint32, error) {
	reader := wire.NewReader(payload)
	jid, err := reader.U32()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return jid, nil
}

// EncodeFriendEventAdded3F9A renders the 0x3F9A case-2 body the client's
// CPSMission_OnFriendConnectNotice0x3F9A reads: u8 2, u32 jid, sized
// name, u32 model.
// The client inserts the record with state 0 (ONLINE) - the server only
// composes this event when both sides are live, so the hardcoded online
// insert is honest.
func EncodeFriendEventAdded3F9A(jid uint32, name string, modelRefID uint32) []byte {
	writer := wire.NewWriter(12 + len(name))
	writer.U8(FriendEventAdded)
	writer.U32(jid)
	writeSizedString(writer, name)
	writer.U32(modelRefID)
	return writer.Payload()
}

// EncodeFriendEventDeleted3F9A renders the 0x3F9A case-3 body
// (client CPSMission_OnFriendConnectNotice0x3F9A): u8 3, u32 jid. The
// client's erase THROWS on
// a jid it does not list - only compose this toward a session whose
// roster is known to carry the jid.
func EncodeFriendEventDeleted3F9A(jid uint32) []byte {
	writer := wire.NewWriter(5)
	writer.U8(FriendEventDeleted)
	writer.U32(jid)
	return writer.Payload()
}

// EncodeFriendEventState3F9A renders the 0x3F9A case-4 body (client
// CPSMission_OnFriendConnectNotice0x3F9A): u8 4, u32 jid, u8 state. The
// client dereferences the roster record UNGUARDED - a state event for
// an unlisted
// jid is a native null-deref, so every send site guards on the
// receiver's persisted roster first.
func EncodeFriendEventState3F9A(jid uint32, state uint8) []byte {
	writer := wire.NewWriter(6)
	writer.U8(FriendEventState)
	writer.U32(jid)
	writer.U8(state)
	return writer.Payload()
}

// FriendJID projects a store character ID onto the u32 wire jid space. The
// authority store enforces domain.MaxCharacterID at load and allocation, so
// this projection cannot alias two characters.
func FriendJID(id int64) uint32 {
	return uint32(id)
}

// FriendModelRef resolves the model refObjId a friend edge carries: the
// same chain the local-player entry uses (explicit ModelRef field, then
// the roster codename lookup, then the race/gender start-profile
// fallback - bootstrap resolveModelRef's shape).
func FriendModelRef(deps Dependencies, c *enterworld.Character) uint32 {
	if c == nil {
		return 0
	}
	return deps.CharacterModelRef(c)
}

// FriendRosterEntries builds the 0x3769 roster rows from the persisted
// edges, deriving each state byte from LIVE presence at encode time (the
// state is never persisted - a reboot honestly reopens everyone offline
// until their session binds again). A nil presence reads all-offline.
func FriendRosterEntries(presence Presence, divisionID string, character *enterworld.Character) []FriendRosterEntry {
	edges := enterworld.FriendsView(character)
	if len(edges) == 0 {
		return nil
	}
	entries := make([]FriendRosterEntry, 0, len(edges))
	for _, edge := range edges {
		state := FriendStateOffline
		if presenceOnline(presence, divisionID, edge.Name) {
			state = FriendStateOnline
		}
		entries = append(entries, FriendRosterEntry{
			JID:        FriendJID(edge.ID),
			Name:       edge.Name,
			ModelRefID: edge.ModelRefID,
			State:      state,
		})
	}
	return entries
}

// findCharacterByName resolves a division character record by name,
// case-insensitively - the same EqualFold match the enter-world Build
// resolves with (CreateCharacter refuses case-insensitive duplicates, so
// the fold is unambiguous).
func findCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

// findCharacterByID resolves a division character record by store ID
// (the friend jid space).
func findCharacterByID(deps Dependencies, divisionID string, id int64) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && candidate.ID == id {
			return candidate
		}
	}
	return nil
}

// friendListed reports whether the character's persisted roster carries
// the id - the guard every 0x3F9A case-3/case-4 send takes before
// composing toward a session (the client folds throw on unlisted jids).
func friendListed(character *enterworld.Character, id int64) bool {
	return friendIndex(character, id) >= 0
}

func friendIndex(character *enterworld.Character, id int64) int {
	for index, edge := range enterworld.FriendsView(character) {
		if edge.ID == id {
			return index
		}
	}
	return -1
}

// appendFriendEdge appends one already-validated edge while the caller holds
// the multi-character update door. Copy-then-swap publishes the new slice
// header to cross-session readers.
func appendFriendEdge(owner *enterworld.Character, edge enterworld.FriendRecord) {
	current := owner.Friends
	next := make([]enterworld.FriendRecord, 0, len(current)+1)
	next = append(next, current...)
	next = append(next, edge)
	enterworld.SwapFriends(owner, next)
}

// removeFriendEdgeAt removes one already-resolved edge while the caller holds
// the multi-character update door.
func removeFriendEdgeAt(owner *enterworld.Character, at int) {
	current := owner.Friends
	next := make([]enterworld.FriendRecord, 0, len(current)-1)
	next = append(next, current[:at]...)
	next = append(next, current[at+1:]...)
	enterworld.SwapFriends(owner, next)
}

// AppendMutualFriendEdges installs both halves of a mutual edge in
// ONE multi-record door: the commit of a consented friend request
// (FriendRequests.Answer). Both lists are re-validated before either
// swap, so a concurrent duplicate/cap change leaves both records untouched.
func AppendMutualFriendEdges(
	deps Dependencies,
	actor, target *enterworld.Character,
) (targetEdge, actorEdge enterworld.FriendRecord, applied bool) {
	applied = deps.UpdateMany([]*enterworld.Character{actor, target}, "friend-add", func() bool {
		if actor.DeletePending || target.DeletePending {
			return false
		}
		targetEdge = enterworld.FriendRecord{
			ID:         target.ID,
			Name:       target.Name,
			ModelRefID: FriendModelRef(deps, target),
		}
		actorEdge = enterworld.FriendRecord{
			ID:         actor.ID,
			Name:       actor.Name,
			ModelRefID: FriendModelRef(deps, actor),
		}
		if len(actor.Friends) >= FriendMaxCount || len(target.Friends) >= FriendMaxCount {
			return false
		}
		for _, edge := range actor.Friends {
			if edge.ID == targetEdge.ID {
				return false
			}
		}
		for _, edge := range target.Friends {
			if edge.ID == actorEdge.ID {
				return false
			}
		}
		// Both validations have passed under the same door. The two
		// copy-then-swap operations cannot refuse, so there is no partial
		// mutation or unwind path.
		appendFriendEdge(actor, targetEdge)
		appendFriendEdge(target, actorEdge)
		return true
	})
	return targetEdge, actorEdge, applied
}

// removeMutualFriendEdgesDoor removes both reciprocal edges in ONE
// multi-record door. Either-side drift refuses without mutation: current
// stores validate mutuality at boot, accepted adds are atomic, and final
// character deletion cascades inbound edges in its archive transaction.
func removeMutualFriendEdgesDoor(
	deps Dependencies,
	actor, target *enterworld.Character,
	targetID int64,
) (actorRemoved, targetRemoved bool) {
	deps.UpdateMany([]*enterworld.Character{actor, target}, "friend-delete", func() bool {
		if actor.DeletePending {
			return false
		}
		actorIndex := friendIndex(actor, targetID)
		targetIndex := friendIndex(target, actor.ID)
		if actorIndex < 0 || targetIndex < 0 {
			return false
		}
		removeFriendEdgeAt(actor, actorIndex)
		removeFriendEdgeAt(target, targetIndex)
		actorRemoved = true
		targetRemoved = true
		return true
	})
	return actorRemoved, targetRemoved
}

// FriendDeleteOutcome is one handled 0x75DB request. On success the
// actor's edge is gone (announce case 3 {FriendJID} to the actor);
// TargetRemoved reports whether the mutual edge was also removed from a
// live target record (announce case 3 {ActorJID} to the target's session
// - guarded, because the client's erase throws on an unlisted jid).
type FriendDeleteOutcome struct {
	Refusal       string
	Code          uint8
	FriendJID     uint32
	ActorJID      uint32
	TargetName    string
	TargetRemoved bool
}

// Delete refusal codes (the reference server's friend delete, shown by the
// v1.150 client's 0xB5DB handler 75CE90 as notice 0x13/code).
const (
	friendDeleteErrRequest   uint8 = 0x02
	friendDeleteErrNotListed uint8 = 0x03
)

func refusedFriendDelete(reason string, code uint8) FriendDeleteOutcome {
	return FriendDeleteOutcome{Refusal: reason, Code: code}
}

// HandleFriendDelete applies one 0x75DB request: remove the edge with
// the requested jid from the actor's roster, and the mutual edge from
// the target's record in the same mutation door. A missing target or
// missing reciprocal edge is authority drift and refuses without mutation.
// A jid the actor does not list refuses silently (the client only
// composes jids off its own roster, so a miss is a desync or a forged
// frame; no refusal bytes are pinned either way).
func HandleFriendDelete(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) FriendDeleteOutcome {
	if actor == nil {
		return refusedFriendDelete("characterNotFound", friendDeleteErrRequest)
	}
	jid, err := DecodeFriendDeleteRequest(payload)
	if err != nil {
		return refusedFriendDelete(err.Error(), friendDeleteErrRequest)
	}
	target := findCharacterByID(deps, divisionID, int64(jid))
	if target == nil {
		return refusedFriendDelete(fmt.Sprintf("friend target %d is not live in division %s", jid, divisionID), friendDeleteErrNotListed)
	}
	actorRemoved, targetRemoved := removeMutualFriendEdgesDoor(deps, actor, target, int64(jid))
	if !actorRemoved {
		return refusedFriendDelete(fmt.Sprintf("jid %d is not a mutual live edge or the actor is delete-pending", jid), friendDeleteErrNotListed)
	}
	outcome := FriendDeleteOutcome{
		FriendJID:     jid,
		ActorJID:      FriendJID(actor.ID),
		TargetRemoved: targetRemoved,
	}
	if target != nil {
		outcome.TargetName = target.Name
	}
	return outcome
}

/*
================
RegisterFriend

Wires the friend lane onto the hub and returns its request owner, which
the 0x3012 consent (enterworld.Deps.FriendConsent) and the session-close
hook reach. 0x7164 asks the target (friendrequest.go); 0x75DB removes the
mutual pair and answers 0xB5DB, success also telling both live sides with
0x3F9A case 3. Identity comes from the session bind only. Sends run
after the Mutate doors return, never inside the store lock.
================
*/
func RegisterFriend(hub *transport.Hub, deps Dependencies, presence Presence) *FriendRequests {
	requests := NewFriendRequests(deps, presence)
	hub.Handle(OpFriendAddRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		requests.Ask(s, divisionID, actor, payload)
	})
	hub.Handle(OpFriendDeleteRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleFriendDelete(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("community: 0x75DB (friend-delete) refused for %s: %s", actor.Name, outcome.Refusal)
			_ = s.Send(OpFriendDeleteAnswer, EncodeFriendDeleteErrorB5DB(outcome.Code))
			return
		}
		_ = s.Send(OpFriendLetterEventPush, EncodeFriendEventDeleted3F9A(outcome.FriendJID))
		_ = s.Send(OpFriendDeleteAnswer, EncodeFriendDeleteOKB5DB(outcome.FriendJID))
		if outcome.TargetRemoved {
			if peer, ok := presenceSession(presence, divisionID, outcome.TargetName); ok {
				_ = peer.Send(OpFriendLetterEventPush, EncodeFriendEventDeleted3F9A(outcome.ActorJID))
			}
		}
		log.Debugf("community: %s removed friend jid %d (%s)", actor.Name, outcome.FriendJID, outcome.TargetName)
	})
	return requests
}

/*
================
FriendRequests.Consent

The enterworld 0x3012 hook: a consent from a bound session.
================
*/
func (r *FriendRequests) Consent(s *transport.Session, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	r.Answer(s, divisionID, actor, payload)
}

// FriendWorldBound is the presence-online fanout, called from the
// server's OnWorldBound tail after the exclusive bind wins: every LIVE
// friend whose persisted roster carries this character receives 0x3F9A
// case 4 state=0 on their own session - the flip the client's contact
// icon and connect announce run without a re-enter. The binder's own
// roster needs no self-push: its 0x3769 seed was encoded with live
// presence moments earlier in the same enter-world. A rebind after an
// eviction fans out again; the client's state write is idempotent.
func FriendWorldBound(deps Dependencies, presence Presence, divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	event := EncodeFriendEventState3F9A(FriendJID(character.ID), FriendStateOnline)
	notifyFriendSessions(deps, presence, divisionID, character, event)
}

// FriendSessionClosed is the presence-offline fanout, called from the
// hub's OnSessionClose hook: resolve the closing session's bound
// character (an evicted loser had its name key cleared at bind time and
// resolves to nothing), skip when ANOTHER session already holds the bind
// key (the rebind winner is live - the character never went offline),
// then push case 4 state=1 to every live friend that lists them.
func FriendSessionClosed(deps Dependencies, presence Presence, s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(deps, s)
	if !bound {
		return
	}
	if winner, live := presenceSession(presence, divisionID, character.Name); live && winner != s {
		return
	}
	event := EncodeFriendEventState3F9A(FriendJID(character.ID), FriendStateOffline)
	notifyFriendSessions(deps, presence, divisionID, character, event)
}

// notifyFriendSessions sends one prebuilt 0x3F9A frame to the live
// session of every friend edge whose OWN persisted roster still lists
// the character (the mutual invariant makes that the normal case; the
// guard exists because the client's case-4 handler null-derefs on an
// unlisted jid, so a half-removed edge must never receive the event).
func notifyFriendSessions(deps Dependencies, presence Presence, divisionID string, character *enterworld.Character, event []byte) {
	// Resolve reciprocal membership under the authority read door, but never
	// hold that door across presence lookup or a network send.
	characters := deps.CharactersForDivision(divisionID)
	var recipients []string
	deps.Read(divisionID, func() {
		for _, edge := range enterworld.FriendsView(character) {
			friend := characterByID(characters, edge.ID)
			if friend == nil || !friendListed(friend, character.ID) {
				continue
			}
			recipients = append(recipients, edge.Name)
		}
	})
	for _, name := range recipients {
		peer, ok := presenceSession(presence, divisionID, name)
		if !ok {
			continue
		}
		_ = peer.Send(OpFriendLetterEventPush, event)
	}
}

func characterByID(characters []*enterworld.Character, id int64) *enterworld.Character {
	for _, character := range characters {
		if character != nil && character.ID == id {
			return character
		}
	}
	return nil
}
