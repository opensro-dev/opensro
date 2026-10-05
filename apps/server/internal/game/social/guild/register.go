package guild

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/transport"
)

// Presence is the narrow live-session view the guild wire adapter consumes.
// The interface lives at the consumer boundary so community can implement it
// without reversing the package dependency.
type Presence interface {
	SessionByName(divisionID, name string) (*transport.Session, bool)
	OnlineByName(divisionID, name string) bool
}

// Register wires the guild lane onto the hub: eight REAL mutators -
// create 0x7663, notice edit 0x777A, kick 0x74B1, leave 0x756E, break
// 0x766E, name grant 0x72BC, position grant 0x765F and GP donate
// 0x740F. 0x73AD (invite) registers on the InviteRuntime instead
// (invite.go - the consent handshake; wiring.go constructs and hooks
// it).
//
// Each opcode registers exactly once and none is registered by any
// other lane (hub registration is last-write-wins; survey: bootstrap
// 0x0006/0x3012/0x707B/0x7427, movement 0x7738/0x7017/0x324B/0x7025/
// 0x769E, action 0x706D/0x72CD/0x745A, progression 0x727A/0x7552/0x7165/
// 0x72CB, community 0x766F/0x7164/0x75DB/0x7261/0x73F2/0x70CC, match
// 0x76FF/0x73DC/0x7535/0x7588/0x755D/0x713E/0x770B/0x7701, party
// 0x70D5/0x751A/0x704F/0x7664). Called from server.go with the SAME
// deps pointer every other lane retains, and the SAME presence facade
// the community lanes fan out through. Every refusal stays SILENT and
// logs its reason EXCEPT the evidenced result=2 arms (errors.go): the
// create name-length refusal answers 0xB663 {2, 0x18} and the notice
// empty-field refusals answer 0xB77A {2, 0x22}/{2, 0x23} to the actor
// only. Notice permission and no-membership replies are also carried by
// B77A; their typed policy lives in NoticeRefusalPayload. Every success send is presence-targeted (never a
// division broadcast). Sends run AFTER the store doors return - never
// inside the store lock.
func Register(hub *transport.Hub, deps Dependencies, presence Presence, unions *UnionRuntime) {
	hub.Handle(OpGuildCreateRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (create) from unbound session %d discarded", opcode, s.ID)
			return
		}
		online := func(name string) bool {
			return presence != nil && presence.OnlineByName(divisionID, name)
		}
		outcome := HandleCreate(deps, divisionID, actor, payload, online)
		if outcome.Refusal != "" {
			if outcome.ErrorPayload != nil {
				_ = s.Send(OpGuildCreateAck, outcome.ErrorPayload)
			}
			log.Debugf("guild: 0x7663 (create) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildCreateAck, outcome.AckPayload)
		// The NPC select plane exists now (action answers 0xB45A on the
		// roster-NPC grant), so this gid COULD be checked against the
		// selection store. DECISION: not enforced yet - the roster
		// carries no guild-capable NPC (simulation.NpcTalkCapabilityFlags
		// grants NpcTalkFlagGuild only to NPC_EU_GUILD, which does not
		// spawn), and dev/e2e creates legitimately send gid 0, so
		// enforcement today would refuse every live create. Revisit when
		// a guild NPC joins the roster.
		log.Debugf("guild: %s created guild %d (selectedTargetGid=%d decoded, not validated - see the enforcement decision above)", actor.Name, outcome.GuildID, outcome.SelectedTargetGid)
	})
	hub.Handle(OpGuildNoticeEditRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (notice-edit) from unbound session %d discarded", opcode, s.ID)
			return
		}
		// Native admission precedes decoding and all publication/mutation.
		if restriction.Report(s, transport.CommandRestrictionChat) {
			return
		}
		outcome := HandleNoticeEdit(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			if outcome.ErrorPayload != nil {
				_ = s.Send(OpGuildNoticeEditAck, outcome.ErrorPayload)
			}
			log.Debugf("guild: 0x777A (notice-edit) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildNoticeEditAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		log.Debugf("guild: %s edited the guild notice (%d member(s) named for the subOp-5 fan-out)", actor.Name, len(outcome.MemberNames))
	})
	hub.Handle(OpGuildKickRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (kick) from unbound session %d discarded", opcode, s.ID)
			return
		}
		guildID := characterGuild(actor)
		outcome := HandleKick(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x74B1 (kick) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		unions.GuildMembersChanged(divisionID, guildID)
		log.Debugf("guild: %s kicked %s (subOp-3 fanned to the online members)", actor.Name, outcome.KickedName)
	})
	hub.Handle(OpGuildLeaveRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (leave) from unbound session %d discarded", opcode, s.ID)
			return
		}
		guildID := characterGuild(actor)
		outcome := HandleLeave(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x756E (leave) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildLeaveAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		unions.GuildMembersChanged(divisionID, guildID)
		log.Debugf("guild: %s left their guild (selectedTargetGid=%d decoded, not validated; subOp-3 kind-1 fanned to %d named member(s))", actor.Name, outcome.SelectedTargetGid, len(outcome.MemberNames))
	})
	hub.Handle(OpGuildBreakRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (break) from unbound session %d discarded", opcode, s.ID)
			return
		}
		guildID := characterGuild(actor)
		outcome := HandleBreak(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x766E (break) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildBreakAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		unions.GuildBroken(divisionID, guildID)
		log.Debugf("guild: %s dissolved their guild (selectedTargetGid=%d decoded, not validated; subOp-1 fanned to %d named member(s))", actor.Name, outcome.SelectedTargetGid, len(outcome.MemberNames))
	})
	hub.Handle(OpGuildNameGrantRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (name-grant) from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleNameGrant(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x72BC (name-grant) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildNameGrantAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		log.Debugf("guild: %s granted a title to %s (0xB2BC acked; subOp-6 &0x20 fanned to %d named member(s))", actor.Name, outcome.TargetName, len(outcome.MemberNames))
	})
	hub.Handle(OpGuildPositionGrantRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (position-grant) from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandlePositionGrant(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x765F (position-grant) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildPositionGrantAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
		log.Debugf("guild: %s granted a fortress position to %s (0xB65F acked; subOp-6 &0x40 fanned to %d named member(s))", actor.Name, outcome.TargetName, len(outcome.MemberNames))
	})
	hub.Handle(OpGuildPermissionRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (permission) from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandlePermissionUpdate(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			if outcome.ErrorPayload != nil {
				_ = s.Send(OpGuildPermissionResult, outcome.ErrorPayload)
			}
			log.Debugf("guild: 0x744E (permission) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.PushPayload)
	})
	hub.Handle(OpGuildGpDonateRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		actor, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("guild: 0x%04X (gp-donate) from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleGpDonate(deps, divisionID, actor, payload)
		if outcome.Refusal != "" {
			log.Debugf("guild: 0x740F (gp-donate) refused for %s: %s", actor.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpGuildGpDonateAck, outcome.AckPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.GuildGpPushPayload)
		sendToOnlineMembers(presence, divisionID, outcome.MemberNames, outcome.DonorGpPushPayload)
		log.Debugf("guild: %s donated GP (0xB40F acked; subOp-5 &0x08 + subOp-6 &0x08 fanned to %d named member(s) including the donor)", actor.Name, len(outcome.MemberNames))
	})
}

// sendToOnlineMembers pushes one prebuilt frame to the live session of
// every named member (presence-targeted; offline names simply miss).
func sendToOnlineMembers(presence Presence, divisionID string, names []string, payload []byte) {
	if presence == nil {
		return
	}
	for _, name := range names {
		if peer, online := presence.SessionByName(divisionID, name); online {
			_ = peer.Send(OpGuildUpdatePush, payload)
		}
	}
}

/*
================
characterGuild

The guild a character stands in before a membership change (0 for none).
================
*/
func characterGuild(c *enterworld.Character) int64 {
	if c == nil || c.GuildID == nil {
		return 0
	}
	return *c.GuildID
}
