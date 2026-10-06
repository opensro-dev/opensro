/*
===========================================================================

union.go - the guild union lane

A guild master proposes a union to the master of another guild
(CGObjPC_HandleUnionInvite70FB 5170B0 -> GuildManager_RequestUnionInvite
5C6550), who answers the 0x3393 kind-6 prompt on the shared consent
multiplex (confirm box 0x1D: {01 01} accepts, {02 00} refuses). A master
leaves a union (5C6710) or, as master of the leading guild, expels a
guild from it (5C67A0). None of the three may run while either guild
fights a fortress war (SiegeManager_IsGuildOutOfWar 635470: 0x4C75,
0x4C76, 0x4C77).

The union state belongs to package union; this lane checks the guilds,
commits through it and tells every guild's online members:

  - a joined guild's members receive the whole list (0x341E), and so do
    the leading guild's when the union is founded (5CA5A0 sends both);
  - every other guild's members receive the new row (0x3B29 0x0D);
  - a guild that leaves, is expelled or breaks up sends 0x3B29 0x12 to
    every guild that stood in the union, itself included, or 0x12 3
    when the union dissolves (CAlliance_RemoveGuild 5B8CF0,
    CAlliance_Clear 5B8DD0);
  - a union guild whose member count changes patches its row in the
    other guilds (0x3B29 0x0E mask 8, CAlliance_ForEachOtherGuild 5B9000).

The proposal's own success sends nothing (5C6550 returns 1 and 70FB
answers only a failure). INFERENCE: the shard's refusal of a full union
at the answer reaches the inviter as 0xB379 0x29.

===========================================================================
*/
package guild

import (
	"errors"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/transport"
)

/*
================
WarStatus

Which guilds fight a running fortress war (fortress.Authority).
================
*/
type WarStatus interface {
	GuildInWar(divisionID string, guildID int64) bool
}

/*
================
pendingUnion

One unanswered union proposal, keyed by the answering master. Both
guilds are re-validated at the answer.
================
*/
type pendingUnion struct {
	inviterName string
	leaderGuild int64
	joinerGuild int64
	expiresAtMs int64
}

/*
================
UnionRuntime
================
*/
type UnionRuntime struct {
	deps      Dependencies
	presence  Presence
	unions    *union.Authority
	wars      WarStatus
	GuildWars *guildwar.Authority

	mu      sync.Mutex
	pending map[string]pendingUnion

	// PeerPending reports another lane's unanswered proposal for a player
	// (one at a time, TransactionMgr_InsertUnique 46F420).
	PeerPending func(divisionID, name string) bool
	Now         func() time.Time
}

/*
================
NewUnionRuntime
================
*/
func NewUnionRuntime(deps Dependencies, presence Presence, unions *union.Authority, wars WarStatus) *UnionRuntime {
	return &UnionRuntime{
		deps:     deps,
		presence: presence,
		unions:   unions,
		wars:     wars,
		pending:  map[string]pendingUnion{},
		Now:      time.Now,
	}
}

/*
================
Register
================
*/
func (r *UnionRuntime) Register(hub *transport.Hub) {
	hub.Handle(OpUnionInvite, r.handleInvite)
	hub.Handle(OpUnionLeave, r.handleLeave)
	hub.Handle(OpUnionKick, r.handleKick)
}

/*
================
Unions
================
*/
func (r *UnionRuntime) Unions() *union.Authority {
	if r == nil {
		return nil
	}
	return r.unions
}

//============================================================================

/*
================
unionActor

The requesting master, their guild and its roster. code is the refusal
when they are not one.
================
*/
func (r *UnionRuntime) unionActor(divisionID string, actor *enterworld.Character) (domain.GuildRecord, []domain.GuildMemberRecord, uint8) {
	if actor == nil || actor.GuildID == nil || r.deps.GuildAuthority() == nil {
		return domain.GuildRecord{}, nil, unionErrNoGuild
	}
	guild, members, ok := r.deps.GuildAuthority().Guild(divisionID, *actor.GuildID)
	if !ok {
		return domain.GuildRecord{}, nil, unionErrNoGuild
	}
	member, ok := memberByCharID(members, actor.ID)
	if !ok {
		return domain.GuildRecord{}, nil, unionErrNoGuild
	}
	if member.Grade != 0 {
		return guild, members, unionErrPermission
	}
	return guild, members, 0
}

/*
================
inWar
================
*/
func (r *UnionRuntime) inWar(divisionID string, guildIDs ...int64) bool {
	if r.wars == nil {
		return false
	}
	for _, guildID := range guildIDs {
		if r.wars.GuildInWar(divisionID, guildID) {
			return true
		}
	}
	return false
}

/*
================
inviteRefusal

GuildManager_RequestUnionInvite 5C6550 in its order. The hostile-guild
refusal (0x4C42) reads the same enemy owner as combat.
================
*/
func (r *UnionRuntime) inviteRefusal(divisionID string, actor *enterworld.Character, targetGid uint32) (pendingUnion, *enterworld.Character, uint8) {
	guild, _, code := r.unionActor(divisionID, actor)
	if code != 0 {
		return pendingUnion{}, nil, code
	}
	if record, ok := r.unions.Of(divisionID, guild.ID); ok && record.Guilds[0] != guild.ID {
		return pendingUnion{}, nil, unionErrPermission
	}
	if guild.Level < unionMinimumGuildLevel {
		return pendingUnion{}, nil, unionErrLevelTooLow
	}
	target := findGuildCharacterByGid(r.deps, divisionID, targetGid)
	if target == nil || target.DeletePending || !r.presence.OnlineByName(divisionID, target.Name) {
		return pendingUnion{}, nil, unionErrTargetInvalid
	}
	if target.GuildID == nil {
		return pendingUnion{}, nil, unionErrTargetNoGuild
	}
	targetGuild, targetMembers, ok := r.deps.GuildAuthority().Guild(divisionID, *target.GuildID)
	if !ok {
		return pendingUnion{}, nil, unionErrTargetNoGuild
	}
	if targetGuild.ID == guild.ID {
		return pendingUnion{}, nil, unionErrOwnGuild
	}
	if targetGuild.Level < unionMinimumGuildLevel {
		return pendingUnion{}, nil, unionErrTargetLevelLow
	}
	if _, ok := r.unions.Of(divisionID, targetGuild.ID); ok {
		return pendingUnion{}, nil, unionErrTargetHasUnion
	}
	if member, ok := memberByCharID(targetMembers, target.ID); !ok || member.Grade != 0 {
		return pendingUnion{}, nil, unionErrTargetNotMaster
	}
	if _, hostile := r.GuildWars.Find(divisionID, guild.ID, targetGuild.ID); hostile {
		return pendingUnion{}, nil, 0x42
	}
	// 4EA0D0: a proposal the transaction manager cannot queue answers 3.
	if r.HasPendingInvite(divisionID, target.Name) || r.PeerPending != nil && r.PeerPending(divisionID, target.Name) {
		return pendingUnion{}, nil, unionErrTargetInvalid
	}
	return pendingUnion{inviterName: actor.Name, leaderGuild: guild.ID, joinerGuild: targetGuild.ID}, target, 0
}

/*
================
handleInvite
================
*/
func (r *UnionRuntime) handleInvite(s *transport.Session, _ uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	targetGid, err := decodeUnionU32(payload)
	if err != nil || actor == nil || actor.DeletePending {
		return
	}
	if actor.GuildID != nil && r.inWar(divisionID, *actor.GuildID) {
		_ = s.Send(OpUnionInviteResult, encodeUnionResult(unionErrInviteDuringWar))
		return
	}
	invite, target, code := r.inviteRefusal(divisionID, actor, targetGid)
	if code == 0 && r.inWar(divisionID, invite.joinerGuild) {
		code = unionErrInviteDuringWar
	}
	if code != 0 {
		_ = s.Send(OpUnionInviteResult, encodeUnionResult(code))
		log.Debugf("guild: union invite from %s refused with %#x", actor.Name, code)
		return
	}
	session, online := r.presence.SessionByName(divisionID, target.Name)
	if !online {
		_ = s.Send(OpUnionInviteResult, encodeUnionResult(unionErrTargetInvalid))
		return
	}
	invite.expiresAtMs = r.Now().UnixMilli() + inviteAnswerWindowMs
	r.mu.Lock()
	r.pending[inviteKey(divisionID, target.Name)] = invite
	r.mu.Unlock()
	_ = session.Send(OpInvitationProposal, EncodeUnionPrompt3393(enterworld.ObjectIDForCharacter(actor)))
}

//============================================================================

/*
================
HasPendingInvite
================
*/
func (r *UnionRuntime) HasPendingInvite(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite, ok := r.pending[inviteKey(divisionID, name)]
	if ok && r.Now().UnixMilli() > invite.expiresAtMs {
		delete(r.pending, inviteKey(divisionID, name))
		return false
	}
	return ok
}

/*
================
DropPendingInvite
================
*/
func (r *UnionRuntime) DropPendingInvite(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inviteKey(divisionID, name)
	_, ok := r.pending[key]
	delete(r.pending, key)
	return ok
}

/*
================
SessionClosed

A prompt dies with its target's session; a rebind winner keeps it.
================
*/
func (r *UnionRuntime) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.presence.SessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	r.DropPendingInvite(divisionID, character.Name)
}

/*
================
ApplyConsent

The answering master's reply. Only {01 01} accepts; both guilds and both
masters are checked again before the union changes.
================
*/
func (r *UnionRuntime) ApplyConsent(_ *transport.Session, divisionID string, actor *enterworld.Character, result, code uint8) {
	if actor == nil || !r.HasPendingInvite(divisionID, actor.Name) {
		return
	}
	r.mu.Lock()
	invite := r.pending[inviteKey(divisionID, actor.Name)]
	delete(r.pending, inviteKey(divisionID, actor.Name))
	r.mu.Unlock()
	if result != ConsentResultAccept || code != ConsentCodeAccept {
		return
	}
	inviter := findGuildCharacterByName(r.deps, divisionID, invite.inviterName)
	inviterSession, online := r.presence.SessionByName(divisionID, invite.inviterName)
	if inviter == nil || !online {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	leader, _, leaderCode := r.unionActor(divisionID, inviter)
	joiner, _, joinerCode := r.unionActor(divisionID, actor)
	if leaderCode != 0 || joinerCode != 0 || leader.ID != invite.leaderGuild || joiner.ID != invite.joinerGuild {
		return
	}
	if r.inWar(divisionID, leader.ID, joiner.ID) {
		_ = inviterSession.Send(OpUnionInviteResult, encodeUnionResult(unionErrInviteDuringWar))
		return
	}
	if _, hostile := r.GuildWars.Find(divisionID, leader.ID, joiner.ID); hostile {
		_ = inviterSession.Send(OpUnionInviteResult, encodeUnionResult(0x42))
		return
	}
	_, founded := r.unions.Of(divisionID, leader.ID)
	founded = !founded
	record, err := r.unions.Join(divisionID, leader.ID, joiner.ID)
	switch {
	case errors.Is(err, union.ErrFull):
		_ = inviterSession.Send(OpUnionInviteResult, encodeUnionResult(unionErrFull))
		return
	case errors.Is(err, union.ErrMember):
		_ = inviterSession.Send(OpUnionInviteResult, encodeUnionResult(unionErrTargetHasUnion))
		return
	case err != nil:
		log.Errorf("guild: union of %d and %d not saved: %v", leader.ID, joiner.ID, err)
		return
	}
	r.publishJoined(divisionID, record, joiner.ID, founded)
	log.Debugf("guild: guild %d joined union %d led by %d", joiner.ID, record.AllianceID, leader.ID)
}

//============================================================================

/*
================
handleLeave

GuildManager_RequestUnionLeave 5C6710.
================
*/
func (r *UnionRuntime) handleLeave(s *transport.Session, _ uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || len(payload) != 0 {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor != nil && actor.GuildID != nil && r.inWar(divisionID, *actor.GuildID) {
		_ = s.Send(OpUnionLeaveResult, encodeUnionResult(unionErrLeaveDuringWar))
		return
	}
	guild, _, code := r.unionActor(divisionID, actor)
	if code == 0 {
		if _, ok := r.unions.Of(divisionID, guild.ID); !ok {
			code = unionErrNotInUnion
		}
	}
	if code != 0 {
		_ = s.Send(OpUnionLeaveResult, encodeUnionResult(code))
		return
	}
	r.removeGuild(divisionID, guild.ID, UnionRemovedLeft)
}

/*
================
handleKick

GuildManager_RequestUnionKick 5C67A0: only the leading guild's master
expels, and only a guild of the union. INFERENCE: the leading guild
cannot expel itself (its master leaves instead), answered as an own-guild
proposal is.
================
*/
func (r *UnionRuntime) handleKick(s *transport.Session, _ uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	target, err := decodeUnionU32(payload)
	if err != nil || actor == nil {
		return
	}
	if actor.GuildID != nil && r.inWar(divisionID, *actor.GuildID, int64(target)) {
		_ = s.Send(OpUnionKickResult, encodeUnionResult(unionErrKickDuringWar))
		return
	}
	guild, _, code := r.unionActor(divisionID, actor)
	record, inUnion := r.unions.Of(divisionID, guild.ID)
	switch {
	case code != 0:
	case !inUnion:
		code = unionErrNotInUnion
	case record.Guilds[0] != guild.ID:
		code = unionErrPermission
	case !union.Holds(record, int64(target)):
		code = unionErrNotAlly
	case int64(target) == guild.ID:
		code = unionErrOwnGuild
	}
	if code != 0 {
		_ = s.Send(OpUnionKickResult, encodeUnionResult(code))
		return
	}
	r.removeGuild(divisionID, int64(target), UnionRemovedExpelled)
}

/*
================
removeGuild

Takes a guild out of its union and tells every guild that stood in it.
================
*/
func (r *UnionRuntime) removeGuild(divisionID string, guildID int64, mode uint8) {
	before, dissolved, err := r.unions.Remove(divisionID, guildID)
	if err != nil {
		if !errors.Is(err, union.ErrMember) {
			log.Errorf("guild: guild %d leaving union not saved: %v", guildID, err)
		}
		return
	}
	payload := EncodeAllyRemoved3B29(mode, guildID)
	if dissolved {
		payload = EncodeAllyRemoved3B29(UnionRemovedDissolved, 0)
	}
	for _, member := range before.Guilds {
		if member != 0 {
			r.sendToGuild(divisionID, member, OpGuildUpdatePush, payload)
		}
	}
}

//============================================================================

/*
================
GuildBroken

A guild of a union dissolved (0x766E): it leaves the union. Its roster
is gone from the store, so only the other guilds hear it; its own members
heard the guild end (0x3B29 1), which clears their union list too.
================
*/
func (r *UnionRuntime) GuildBroken(divisionID string, guildID int64) {
	if r == nil {
		return
	}
	r.removeGuild(divisionID, guildID, UnionRemovedLeft)
}

/*
================
GuildMembersChanged

A union guild's member count changed: the other guilds patch its row.
================
*/
func (r *UnionRuntime) GuildMembersChanged(divisionID string, guildID int64) {
	if r == nil {
		return
	}
	record, ok := r.unions.Of(divisionID, guildID)
	if !ok {
		return
	}
	row, ok := r.guildRow(divisionID, guildID)
	if !ok {
		return
	}
	payload := EncodeAllyPatch3B29(row, unionRowMembers)
	for _, other := range record.Guilds {
		if other != 0 && other != guildID {
			r.sendToGuild(divisionID, other, OpGuildUpdatePush, payload)
		}
	}
}

/*
================
publishJoined
================
*/
func (r *UnionRuntime) publishJoined(divisionID string, record domain.AllianceRecord, joiner int64, founded bool) {
	rows := r.unionRows(divisionID, record)
	list := EncodeUnionList341E(record, rows)
	var joined []byte
	for _, row := range rows {
		if row.GuildID == joiner {
			joined = EncodeAllyJoined3B29(row)
		}
	}
	for _, guildID := range record.Guilds {
		switch {
		case guildID == 0:
		case guildID == joiner || founded && guildID == record.Guilds[0]:
			r.sendToGuild(divisionID, guildID, OpUnionList, list)
		case joined != nil:
			r.sendToGuild(divisionID, guildID, OpGuildUpdatePush, joined)
		}
	}
}

/*
================
guildRow
================
*/
func (r *UnionRuntime) guildRow(divisionID string, guildID int64) (UnionGuildRow, bool) {
	if r.deps.GuildAuthority() == nil {
		return UnionGuildRow{}, false
	}
	guild, members, ok := r.deps.GuildAuthority().Guild(divisionID, guildID)
	if !ok {
		return UnionGuildRow{}, false
	}
	return unionGuildRow(guild, members), true
}

/*
================
unionRows

A row per stored guild of the union, in slot order.
================
*/
func (r *UnionRuntime) unionRows(divisionID string, record domain.AllianceRecord) []UnionGuildRow {
	var rows []UnionGuildRow
	for _, guildID := range record.Guilds {
		if guildID == 0 {
			continue
		}
		if row, ok := r.guildRow(divisionID, guildID); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

/*
================
sendToGuild

One frame to every online member of a guild.
================
*/
func (r *UnionRuntime) sendToGuild(divisionID string, guildID int64, opcode uint16, payload []byte) {
	if r.deps.GuildAuthority() == nil || r.presence == nil {
		return
	}
	_, members, ok := r.deps.GuildAuthority().Guild(divisionID, guildID)
	if !ok {
		return
	}
	for _, member := range members {
		if peer, online := r.presence.SessionByName(divisionID, member.Name); online {
			_ = peer.Send(opcode, payload)
		}
	}
}

/*
================
SeedFrame

The union list a guild member enters the world with, when their guild
stands in a union.
================
*/
func (r *UnionRuntime) SeedFrame(divisionID string, character *domain.Character) (enterworld.Packet, bool) {
	if r == nil || character == nil || character.GuildID == nil {
		return enterworld.Packet{}, false
	}
	record, ok := r.unions.Of(divisionID, *character.GuildID)
	if !ok {
		return enterworld.Packet{}, false
	}
	return enterworld.NewPacket(OpUnionList, EncodeUnionList341E(record, r.unionRows(divisionID, record))), true
}

/*
================
UnionChatAudience

The online members a union line reaches: in every guild of the sender's
union, the master and every member holding the union chat right
(permMask 4, CGuild_UnionChatWithinLimit 5C49A0). code is the chat
refusal: 0x0C outside a union, 0x0E without the right
(CGObjPC_OnChatRequest 4B1750 case 0xA). INFERENCE: the shard's
delivery is not in the GameServer; a line reaches the members who may
also speak in it.
================
*/
func (r *UnionRuntime) UnionChatAudience(divisionID string, sender *enterworld.Character) ([]string, uint8) {
	if r == nil || sender == nil || sender.GuildID == nil || r.deps.GuildAuthority() == nil {
		return nil, unionChatNoUnion
	}
	record, ok := r.unions.Of(divisionID, *sender.GuildID)
	if !ok {
		return nil, unionChatNoUnion
	}
	_, members, ok := r.deps.GuildAuthority().Guild(divisionID, *sender.GuildID)
	if !ok {
		return nil, unionChatNoUnion
	}
	if member, ok := memberByCharID(members, sender.ID); !ok || !unionChatRight(member) {
		return nil, unionChatNoRight
	}
	var names []string
	for _, guildID := range record.Guilds {
		if guildID == 0 {
			continue
		}
		_, roster, ok := r.deps.GuildAuthority().Guild(divisionID, guildID)
		if !ok {
			continue
		}
		for _, member := range roster {
			if unionChatRight(member) && r.presence.OnlineByName(divisionID, member.Name) && !strings.EqualFold(member.Name, sender.Name) {
				names = append(names, member.Name)
			}
		}
	}
	return names, 0
}

const (
	// PermMaskUnionChat is the member right to speak in union chat
	// (5C49A0 counts mask & 4).
	PermMaskUnionChat uint32 = 0x4
	unionChatNoUnion  uint8  = 0x0C
	unionChatNoRight  uint8  = 0x0E
)

/*
================
unionChatRight

The master always speaks (4B1750 compares the guild's master id first).
================
*/
func unionChatRight(member domain.GuildMemberRecord) bool {
	return member.Grade == 0 || member.PermMask&PermMaskUnionChat != 0
}
