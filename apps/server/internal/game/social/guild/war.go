/*
===========================================================================

war.go - native guild-war declarations and mutual consent

Client 704390 sends 771B, 701530 sends 7465, and 760800 reads kind-10
proposals on 3393. Pending proposals belong to the answering session; the
durable guildwar authority begins only after both masters pass admission.

===========================================================================
*/
package guild

import (
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/transport"
)

const (
	OpGuildWarDeclare         uint16 = 0x771b
	OpGuildWarDeclareResult   uint16 = 0xb71b
	OpGuildWarSurrender       uint16 = 0x7465
	OpGuildWarSurrenderResult uint16 = 0xb465
	OpGuildWarSeed            uint16 = 0x32bb
	warProposalKind           uint8  = 10
)

/*
================
pendingWar
================
*/
type pendingWar struct {
	inviter       string
	sourceSession *transport.Session
	targetSession *transport.Session
	masters       [2]int64
	guilds        [2]int64
	terms         domain.GuildWarTerms
	expires       int64
}

/*
================
WarRuntime

Near uses the movement owner's current 3D positions and packed world IDs.
Publication is serialized with mutations, so end cannot overtake begin.
================
*/
type WarRuntime struct {
	deps        Dependencies
	presence    Presence
	unions      *UnionRuntime
	Authority   *guildwar.Authority
	mu          sync.Mutex
	pending     map[string]pendingWar
	ending      []pendingWarEnd
	PeerPending func(division, name string) bool
	Near        func(division string, first, second *enterworld.Character) bool
	InFortress  func(division string, character *enterworld.Character) bool
	Now         func() time.Time
}

/*
================
NewWarRuntime
================
*/
func NewWarRuntime(deps Dependencies, presence Presence, unions *UnionRuntime, authority *guildwar.Authority) *WarRuntime {
	return &WarRuntime{deps: deps, presence: presence, unions: unions, Authority: authority, pending: make(map[string]pendingWar), Now: time.Now}
}

/*
================
Register
================
*/
func (r *WarRuntime) Register(hub *transport.Hub) {
	hub.Handle(OpGuildWarDeclare, r.handleWarDeclare)
	hub.Handle(OpGuildWarSurrender, r.handleWarSurrender)
}

/*
================
readWarDeclaration
================
*/
func readWarDeclaration(payload []byte) (name string, terms domain.GuildWarTerms, valid bool) {
	r := wire.NewReader(payload)
	var err error
	if name, err = r.Str(); err != nil {
		return
	}
	if terms.Type, err = r.U8(); err != nil {
		return
	}
	if terms.Period, err = r.U32(); err != nil {
		return
	}
	if terms.ScoreIndex, err = r.U8(); err != nil {
		return
	}
	if terms.Stake, err = r.U32(); err != nil {
		return
	}
	valid = r.Remaining() == 0
	return
}

/*
================
masterByGuildName
================
*/
func (r *WarRuntime) masterByGuildName(division, name string) *enterworld.Character {
	seen := make(map[int64]bool)
	for _, c := range r.deps.CharactersForDivision(division) {
		c = characterSnapshot(r.deps, division, c)
		if c == nil || c.GuildID == nil || seen[*c.GuildID] {
			continue
		}
		seen[*c.GuildID] = true
		guild, members, ok := r.deps.GuildAuthority().Guild(division, *c.GuildID)
		if !ok || !strings.EqualFold(guild.Name, name) {
			continue
		}
		for _, member := range members {
			if member.Grade == LeaderGrade {
				return findGuildCharacterByName(r.deps, division, member.Name)
			}
		}
	}
	return nil
}

/*
================
warAdmission

5C6B60's declaration order. 5C8510 separately admits consent.
================
*/
func (r *WarRuntime) warAdmission(division string, actor, target *enterworld.Character, terms domain.GuildWarTerms) (pendingWar, uint8) {
	var proposal pendingWar
	if target == nil || target.DeletePending {
		return proposal, 0x25
	}
	if !r.presence.OnlineByName(division, target.Name) {
		return proposal, 0x60
	}
	if r.Near == nil || !r.Near(division, actor, target) {
		return proposal, 0x60
	}
	if actor == nil || actor.GuildID == nil {
		return proposal, 0x0d
	}
	if !guildwar.ValidTerms(terms) {
		return proposal, 2
	}
	if target.GuildID == nil || *actor.GuildID == *target.GuildID {
		return proposal, 3
	}
	first, _, code := r.unions.unionActor(division, actor)
	if code != 0 {
		return proposal, code
	}
	second, _, code := r.unions.unionActor(division, target)
	if code != 0 {
		return proposal, code
	}
	if actor.Gold == nil || *actor.Gold < int64(terms.Stake) {
		return proposal, 0x43
	}
	if terms.Stake > guildwar.MaximumStake {
		return proposal, 0x5e
	}
	if len(r.Authority.Wars(division, first.ID)) >= guildwar.MaximumEnemies {
		return proposal, 0x3e
	}
	if len(r.Authority.Wars(division, second.ID)) >= guildwar.MaximumEnemies {
		return proposal, 0x3f
	}
	if _, exists := r.Authority.Find(division, first.ID, second.ID); exists {
		return proposal, 0x3a
	}
	if a, ok := r.unions.unions.Of(division, first.ID); ok {
		if b, ok := r.unions.unions.Of(division, second.ID); ok && a.AllianceID == b.AllianceID {
			return proposal, 0x41
		}
	}
	// 648220 tests the current world's fortress record, not guild enrolment.
	if r.InFortress != nil && r.InFortress(division, actor) && r.InFortress(division, target) {
		return proposal, 0x79
	}
	proposal = pendingWar{inviter: actor.Name, masters: [2]int64{actor.ID, target.ID}, guilds: [2]int64{first.ID, second.ID}, terms: terms}
	return proposal, 0
}

/*
================
handleWarDeclare
================
*/
func (r *WarRuntime) handleWarDeclare(s *transport.Session, _ uint16, payload []byte) {
	actor, division, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	name, terms, valid := readWarDeclaration(payload)
	if !valid {
		return
	}
	actor = characterSnapshot(r.deps, division, actor)
	target := r.masterByGuildName(division, name)
	proposal, code := r.warAdmission(division, actor, target, terms)
	if code == 0 && (r.HasPendingInvite(division, target.Name) || r.PeerPending != nil && r.PeerPending(division, target.Name)) {
		code = 3
	}
	if code != 0 {
		_ = s.Send(OpGuildWarDeclareResult, []byte{2, code})
		return
	}
	session, online := r.presence.SessionByName(division, target.Name)
	if !online {
		_ = s.Send(OpGuildWarDeclareResult, []byte{2, 0x25})
		return
	}
	proposal.sourceSession, proposal.targetSession = s, session
	proposal.expires = r.Now().UnixMilli() + inviteAnswerWindowMs
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, occupied := r.pending[inviteKey(division, target.Name)]; occupied {
		_ = s.Send(OpGuildWarDeclareResult, []byte{2, 3})
		return
	}
	r.pending[inviteKey(division, target.Name)] = proposal
	guild, _, _ := r.deps.GuildAuthority().Guild(division, proposal.guilds[0])
	w := wire.NewWriter(32).U8(warProposalKind).U32(enterworld.ObjectIDForCharacter(actor)).Str(guild.Name)
	w.U8(terms.Type).U32(terms.Period).U8(terms.ScoreIndex).U32(terms.Stake)
	_ = session.Send(OpInvitationProposal, w.Payload())
}

/*
================
HasPendingInvite
================
*/
func (r *WarRuntime) HasPendingInvite(division, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inviteKey(division, name)
	row, ok := r.pending[key]
	if ok && r.Now().UnixMilli() > row.expires {
		delete(r.pending, key)
		r.expireProposal(division, row)
		return false
	}
	return ok
}

/*
================
DropPendingInvite
================
*/
func (r *WarRuntime) DropPendingInvite(division, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inviteKey(division, name)
	_, found := r.pending[key]
	delete(r.pending, key)
	return found
}

/*
================
SessionClosed
================
*/
func (r *WarRuntime) SessionClosed(s *transport.Session) {
	c, division, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	if winner, live := r.presence.SessionByName(division, c.Name); live && winner != s {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, row := range r.pending {
		if row.sourceSession == s || row.targetSession == s {
			delete(r.pending, key)
		}
	}
}

/*
================
ApplyConsent
================
*/
func (r *WarRuntime) ApplyConsent(s *transport.Session, division string, actor *enterworld.Character, result, code uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if actor == nil {
		return
	}
	key := inviteKey(division, actor.Name)
	proposal, ok := r.pending[key]
	if !ok || proposal.targetSession != s {
		return
	}
	delete(r.pending, key)
	if r.Now().UnixMilli() > proposal.expires {
		r.expireProposal(division, proposal)
		return
	}
	inviter := findGuildCharacterByName(r.deps, division, proposal.inviter)
	inviterSession, online := r.presence.SessionByName(division, proposal.inviter)
	if !online || inviter == nil || inviterSession != proposal.sourceSession {
		return
	}
	if result != ConsentResultAccept || code != ConsentCodeAccept {
		_ = inviterSession.Send(OpGuildWarDeclareResult, []byte{2, 0x16})
		return
	}
	actor = characterSnapshot(r.deps, division, actor)
	// 5C8510 repeats master and funds checks, without another range check.
	refusal := uint8(0)
	first, _, firstCode := r.unions.unionActor(division, inviter)
	second, _, secondCode := r.unions.unionActor(division, actor)
	if firstCode != 0 || first.ID != proposal.guilds[0] || inviter.ID != proposal.masters[0] {
		refusal = 0x1e
	} else if secondCode != 0 || second.ID != proposal.guilds[1] || actor.ID != proposal.masters[1] {
		refusal = 0x24
	} else if actor.Gold == nil || *actor.Gold < int64(proposal.terms.Stake) {
		refusal = 0x0c
	} else if inviter.Gold == nil || *inviter.Gold < int64(proposal.terms.Stake) {
		refusal = 0x43
	}
	if refusal != 0 {
		_ = inviterSession.Send(OpGuildWarDeclareResult, []byte{2, refusal})
		return
	}
	now := r.Now().UnixMilli()
	war, refusal, err := r.Authority.Begin(domain.GuildWarStart{Masters: proposal.masters, Record: domain.GuildWarRecord{
		Guilds: proposal.guilds, Type: proposal.terms.Type, ScoreIndex: proposal.terms.ScoreIndex,
		Stake: proposal.terms.Stake * 2, EndMs: guildwar.Deadline(proposal.terms.Period, now),
	}})
	if err != nil {
		log.WithError(err).Error("guild war begin failed")
		refusal = 2
	}
	if refusal != 0 {
		_ = inviterSession.Send(OpGuildWarDeclareResult, []byte{2, refusal})
		return
	}
	for i, session := range []*transport.Session{inviterSession, s} {
		master := inviter
		if i == 1 {
			master = actor
		}
		fresh := findGuildCharacterByName(r.deps, division, master.Name)
		if fresh != nil && fresh.Gold != nil {
			_ = session.Send(wire.OpPointsUpdate, wire.GoldRefresh{Balance: uint64(*fresh.Gold)}.Encode())
		}
	}
	r.publishWarBegin(division, war, now)
	_ = inviterSession.Send(OpGuildWarDeclareResult, []byte{1})
}

/*
================
expireProposal

46CE20 ends an unanswered transaction with 4C10. Only the initiating live
session receives the receipt; a reconnect does not inherit the transaction.
================
*/
func (r *WarRuntime) expireProposal(division string, row pendingWar) {
	if session, online := r.presence.SessionByName(division, row.inviter); online && session == row.sourceSession {
		_ = session.Send(OpGuildWarDeclareResult, []byte{2, 0x10})
	}
}
