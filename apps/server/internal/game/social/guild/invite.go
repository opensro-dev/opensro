package guild

// The guild INVITE HANDSHAKE (backlog T55): 0x73AD holds a pending
// invite and prompts the target with 0x3393 {u8 5, u32 inviterRef}
// (the sub_7644e0 guild arm); membership commits ONLY on the target's
// 0x3393 {01 01} accept (sub_6971b0 case 0xc) through the ATOMIC
// AddGuildMemberAs door, then the joiner gets the full 0x32C4 block and
// every sitting online member the 0x3B29 subOp-2 join row. The party
// lane owns the single 0x3393 hub registration and routes replies here
// by pending-invite ownership (the reply's first byte is a RESULT code,
// not the proposal type - wire.go pins the bytes); wiring.go hooks this
// runtime in as the party lane's guild consent arm. Every refusal on
// every arm stays SILENT on the wire - no invite ack or refusal frame
// is pinned (the errors.go posture: a code without an evidenced
// carrier is not implementable).

import (
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// PendingInvite is one outstanding guild invitation: stored when the
// 0x73AD proposal sends the 0x3393 type-5 prompt, consumed by the
// target's consent. Everything is re-validated at consent time - the
// guild world may have changed while the prompt was up.
type PendingInvite struct {
	// InviterName resolves the inviter at consent time (the party
	// lane's shape - never a session pointer, which could be replaced).
	InviterName string
	// GuildID is the guild the prompt proposed. The consent commits
	// into THIS guild only: an inviter who changed guilds mid-prompt
	// invalidates the invitation.
	GuildID int64

	expiresAtMs int64
}

// InviteRuntime owns the guild lane's pending-invitation table - the
// ONLY in-memory state this lane holds (guild membership itself is
// store-persisted; invitations are session-scoped runtime state that
// legitimately dies on process reboot, the party registry's state
// verdict).
type InviteRuntime struct {
	deps     Dependencies
	presence Presence

	mu              sync.Mutex
	pendingByTarget map[string]PendingInvite

	// PeerPending reports another lane's unanswered proposal for a
	// player. A player holds one at a time (TransactionMgr_InsertUnique
	// 46F420), so this lane then refuses. wiring.go points it at the party
	// registry and the other lanes (a func field, not an import).
	PeerPending func(divisionID, name string) bool

	// Now is the clock the 30 s answer window runs on.
	Now func() time.Time

	// Unions patches the joined guild's union row (nil: no union lane).
	Unions *UnionRuntime
}

// NewInviteRuntime builds the runtime over an empty pending table and retains
// the shared deps pointer. Construct it before lifecycle closures capture the
// runtime itself.
func NewInviteRuntime(deps Dependencies, presence Presence) *InviteRuntime {
	return &InviteRuntime{
		deps:            deps,
		presence:        presence,
		pendingByTarget: make(map[string]PendingInvite),
		Now:             time.Now,
	}
}

// inviteAnswerWindowMs is the transaction timeout (Transaction_Construct
// 46C6C0 stores 30 s); 46F1E0 expires a proposal after it, not at it.
const inviteAnswerWindowMs = 30 * 1000

// expired reports an invitation past its answer window. The caller holds mu.
func (r *InviteRuntime) expired(invite PendingInvite) bool {
	return r.Now().UnixMilli() > invite.expiresAtMs
}

// inviteKey is the pending table's target identity: divisionID + ":" +
// lowercase(name) - the SAME shape as the hub's exclusive bind key and
// the party registry's memberKey, so a session and a character name
// resolve to the same invitation (duplicated here because the import
// points party -> community -> guild, the GuildJID precedent).
func inviteKey(divisionID, name string) string {
	return divisionID + ":" + strings.ToLower(name)
}

// Register wires the invite request onto the hub: 0x73AD ONLY. The
// 0x3393 consent reply deliberately does NOT register here - the party
// lane owns that shared registration and routes guild consents into
// ApplyConsent by pending-invite ownership (last-write-wins hub
// registration means a second Handle(0x3393) would BREAK party consent).
func (r *InviteRuntime) Register(hub *transport.Hub) {
	hub.Handle(OpGuildInviteRequest, r.handleInvite)
}

// PendingInviteCount reports the number of outstanding invitations (the
// reboot-empty and leak assertions).
func (r *InviteRuntime) PendingInviteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendingByTarget)
}

// setPending records the outstanding invitation for a target and starts
// its answer window. handleInvite never calls it over a live proposal.
func (r *InviteRuntime) setPending(divisionID, targetName string, invite PendingInvite) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite.expiresAtMs = r.Now().UnixMilli() + inviteAnswerWindowMs
	r.pendingByTarget[inviteKey(divisionID, targetName)] = invite
}

// takePending consumes the target's outstanding invitation. A consent
// with no pending record (never invited, already answered, or a
// duplicate/stale frame) reports false and the caller drops silently.
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

// HasPendingInvite reports whether an invitation targets the character -
// the party lane's consent router asks it to decide reply ownership
// (party.ConsentArm, implemented structurally).
func (r *InviteRuntime) HasPendingInvite(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	invite, ok := r.pendingByTarget[inviteKey(divisionID, name)]
	return ok && !r.expired(invite)
}

// DropPendingInvite clears the invitation targeting a character when
// their session ends. Reports whether one dropped.
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

// findGuildCharacterByGid resolves an invite target's world gid back
// onto the division character record: the 0x73AD char-ref is the SAME
// 100000+ID band enterworld.ObjectIDForCharacter latches at enter-world
// (the party lane's findCharacterByGid, duplicated because the import
// points the other way).
func findGuildCharacterByGid(deps Dependencies, divisionID string, gid uint32) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && enterworld.ObjectIDForCharacter(candidate) == gid {
			return characterSnapshot(deps, divisionID, candidate)
		}
	}
	return nil
}

// findGuildCharacterByName resolves a division character record by
// name, case-insensitively (the party lane's findCharacterByName twin).
func findGuildCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return characterSnapshot(deps, divisionID, candidate)
		}
	}
	return nil
}

// handleInvite applies one 0x73AD proposal: the actor must be a guild
// member holding the invite permission bit (PermMaskInvite - the client
// arms the Invite command button on mask & 0x1, sub_5e1e80 @0x005e2047;
// mask-only like every other guild permission, no grade bypass), the
// target must resolve by world gid within the actor's division, be
// someone else, not delete-pending, ONLINE (the prompt needs a live
// session) and guildless, and the member count must sit under the u8
// wire cap. Then the target gets the 0x3393 type-5 prompt and the
// proposal parks in the pending table - NOTHING commits here; the
// membership moves only when the target's accept consent arrives. Every
// refusal stays silent (no invite refusal frame is pinned).
func (r *InviteRuntime) handleInvite(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		log.Debugf("guild: 0x%04X (invite) from unbound session %d discarded", opcode, s.ID)
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending {
		log.Debugf("guild: 0x%04X (invite) from unavailable character on session %d discarded", opcode, s.ID)
		return
	}
	targetRef, err := DecodeInviteRequest(payload)
	if err != nil {
		log.Debugf("guild: 0x73AD (invite) malformed from %s: %v", actor.Name, err)
		return
	}
	if r.deps.GuildAuthority() == nil {
		log.Debugf("guild: 0x73AD (invite) refused for %s: no guild store wired", actor.Name)
		return
	}
	if actor.GuildID == nil {
		log.Debugf("guild: 0x73AD (invite) refused for %s: not in a guild", actor.Name)
		return
	}
	guildID := *actor.GuildID
	record, members, ok := r.deps.GuildAuthority().Guild(divisionID, guildID)
	if !ok {
		log.Debugf("guild: 0x73AD (invite) refused for %s: guildId %d resolves to no stored guild", actor.Name, guildID)
		return
	}
	actorMember, ok := memberByCharID(members, actor.ID)
	if !ok {
		log.Debugf("guild: 0x73AD (invite) refused for %s: actor is not a member of guild %d", actor.Name, guildID)
		return
	}
	if actorMember.PermMask&PermMaskInvite == 0 {
		log.Debugf("guild: 0x73AD (invite) refused for %s: permMask %#x lacks the invite bit %#x", actor.Name, actorMember.PermMask, PermMaskInvite)
		return
	}
	if len(members) >= min(GuildWireMemberCap, domain.GuildMemberCapacity(record.Level)) {
		log.Debugf("guild: 0x73AD (invite) refused for %s: guild %d is full at level %d", actor.Name, guildID, record.Level)
		return
	}
	target := findGuildCharacterByGid(r.deps, divisionID, targetRef)
	if target == nil {
		log.Debugf("guild: 0x73AD (invite) refused for %s: target gid %d not in the division", actor.Name, targetRef)
		return
	}
	if target.ID == actor.ID {
		// The client refuses a self-target before composing
		// (@0x00700ba7) - a frame that carries one anyway is a desync
		// or forged.
		log.Debugf("guild: 0x73AD (invite) refused for %s: cannot invite yourself", actor.Name)
		return
	}
	if target.DeletePending {
		log.Debugf("guild: 0x73AD (invite) refused for %s: target %s is delete-pending", actor.Name, target.Name)
		return
	}
	if target.GuildID != nil {
		log.Debugf("guild: 0x73AD (invite) refused for %s: target %s already in guild %d", actor.Name, target.Name, *target.GuildID)
		return
	}
	targetSession, online := r.presence.SessionByName(divisionID, target.Name)
	if !online {
		log.Debugf("guild: 0x73AD (invite) refused for %s: target %s is offline", actor.Name, target.Name)
		return
	}
	// One unanswered proposal per player (46F420). Native fails this one
	// on 0xB0F3 {2, 2}; no v1.150 carrier is pinned, so it stays silent.
	if r.HasPendingInvite(divisionID, target.Name) || r.PeerPending != nil && r.PeerPending(divisionID, target.Name) {
		log.Debugf("guild: 0x73AD (invite) refused for %s: %s already has a proposal waiting", actor.Name, target.Name)
		return
	}
	r.setPending(divisionID, target.Name, PendingInvite{
		InviterName: actor.Name,
		GuildID:     guildID,
	})
	_ = targetSession.Send(OpInvitationProposal, EncodeInvitePrompt3393(enterworld.ObjectIDForCharacter(actor)))
	log.Debugf("guild: %s proposed guild %d to %s - type-5 prompt sent", actor.Name, guildID, target.Name)
}

// ApplyConsent resolves one routed 0x3393 reply against the pending
// table (the party lane's router hands replies here when THIS lane
// holds the target's pending invitation). Every edge stays SILENT on
// the wire:
//
//   - no outstanding invitation (already answered, a duplicate frame,
//     or a prompt that died with the previous session) -> drop;
//   - anything but the exact {01 01} accept pair (the {02 16} refuse,
//     or any forged shape) -> the pending invitation is consumed,
//     nothing emits;
//   - inviter logged off -> refused;
//   - every durable membership, lifecycle, permission, expected-guild,
//     and roster-cap decision is made once by AddGuildMemberAs while the
//     authority store owns the aggregate lock.
//
// On the committed accept the joiner receives the full 0x32C4 block
// (their client holds no guild entry block) and every sitting online
// member the 0x3B29 subOp-2 join row.
func (r *InviteRuntime) ApplyConsent(s *transport.Session, divisionID string, actor *enterworld.Character, result, code uint8) {
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending {
		return
	}
	invite, outstanding := r.takePending(divisionID, actor.Name)
	if !outstanding {
		log.Debugf("guild: 0x3393 consent from %s dropped: no outstanding guild invitation", actor.Name)
		return
	}
	if result != ConsentResultAccept || code != ConsentCodeAccept {
		log.Debugf("guild: %s refused %s's guild invitation ({%d %#x}) - nothing emitted", actor.Name, invite.InviterName, result, code)
		return
	}
	if r.deps.GuildAuthority() == nil {
		log.Debugf("guild: 0x3393 accept from %s dropped: no guild store wired", actor.Name)
		return
	}
	inviter := findGuildCharacterByName(r.deps, divisionID, invite.InviterName)
	if inviter == nil {
		log.Debugf("guild: 0x3393 accept from %s dropped: inviter %s no longer resolvable", actor.Name, invite.InviterName)
		return
	}
	if !r.presence.OnlineByName(divisionID, inviter.Name) {
		// DECISION (the party lane's consent posture): an inviter who
		// logged off mid-prompt invalidates the invitation - the
		// conservative reading where no native behavior is pinned.
		log.Debugf("guild: 0x3393 accept from %s dropped: inviter %s logged off", actor.Name, invite.InviterName)
		return
	}
	// The joiner's initial member row. NOT EVIDENCED - no retail join
	// answer was ever captured - so the values are documented DECISIONS
	// (the HandleCreate posture): grade JoinerGrade (0x0a, the only
	// non-leader grade with an authored rank label), permMask
	// JoinerPermMask (no permission bits - the conservative floor),
	// donatedGP 0, dwords 0, empty grantName, fortressRole 0; level =
	// the persisted level, refObjID = the model resolve chain, JID =
	// GuildJID(actor.ID) - the same projections every guild row takes.
	row := enterworld.GuildMemberRecord{
		CharID:       actor.ID,
		JID:          GuildJID(actor.ID),
		Name:         actor.Name,
		Grade:        JoinerGrade,
		Level:        memberLevel(actor),
		DonatedGP:    0,
		PermMask:     JoinerPermMask,
		GrantName:    "",
		RefObjID:     r.deps.CharacterModelRef(actor),
		FortressRole: 0,
	}
	joinedGuild, refusal := r.deps.GuildAuthority().AddGuildMemberAs(
		divisionID,
		invite.GuildID,
		inviter.ID,
		PermMaskInvite,
		row,
	)
	if refusal.Refused() {
		log.Debugf(
			"guild: 0x3393 accept from %s not committed: %s",
			actor.Name,
			guildRefusalReason(refusal),
		)
		return
	}
	guild := joinedGuild.Guild
	joined := joinedGuild.Members
	online := func(name string) bool {
		return r.presence != nil && r.presence.OnlineByName(divisionID, name)
	}
	_ = s.Send(OpGuildInfo, EncodeGuildInfo32C4(guild, joined, online, r.Now().UnixMilli()))
	joinPush := EncodeMemberJoin3B29(row, online)
	for _, member := range joined {
		if member.CharID == actor.ID {
			continue
		}
		if peer, live := r.presence.SessionByName(divisionID, member.Name); live {
			_ = peer.Send(OpGuildUpdatePush, joinPush)
		}
	}
	r.Unions.GuildMembersChanged(divisionID, invite.GuildID)
	log.Debugf("guild: %s joined guild %d on %s's invitation (%d member(s))", actor.Name, invite.GuildID, inviter.Name, len(joined))
}

// WorldBound is the enter-world hook, called from the server's
// OnWorldBound tail on the winner path: a prompt the PREVIOUS session
// received died with it - the fresh client shows no box, so a consent
// from the new session must find nothing outstanding (the party lane's
// session-boundary rule).
func (r *InviteRuntime) WorldBound(divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	if r.DropPendingInvite(divisionID, character.Name) {
		log.Debugf("guild: %s re-entered the world; stale pending guild invitation dropped", character.Name)
	}
}

// SessionClosed is the disconnect hook, called from the hub's
// OnSessionClose: resolve the closing session's bound character, skip
// when ANOTHER session already holds the bind key (the rebind winner is
// live - the party lane's loser-close guard), then drop any invitation
// targeting them - the prompt died with the target's transport. An
// inviter's disconnect deliberately drops nothing here: the consent-time
// re-validation refuses a commit whose inviter is gone.
func (r *InviteRuntime) SessionClosed(s *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.presence.SessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	if r.DropPendingInvite(divisionID, character.Name) {
		log.Debugf("guild: %s disconnected; pending guild invitation dropped", character.Name)
	}
}
