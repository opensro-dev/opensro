package guild

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

const (
	// GuildNameGrantMinLevel is the guild level the client's name-grant
	// arm requires.
	GuildNameGrantMinLevel uint8 = 4

	// GrantNameMaxBytes shares the guild-name input family's byte cap.
	GrantNameMaxBytes = GuildNameMaxBytes

	FortressRoleNone      uint8 = 0
	FortressRoleCommander       = domain.GuildFortressRoleCommander
	FortressRoleDeputy    uint8 = 0x02
	FortressRoleEngineer  uint8 = 0x04
	FortressRoleGuard     uint8 = 0x08
	FortressRoleElite     uint8 = 0x10
	FortressRoleMercenary uint8 = 0x20
)

// grantOutcome is one handled grant request (0x72BC / 0x765F share the
// shape). AckPayload is the second-host answer for the GRANTING ACTOR
// ONLY (0xB2BC / 0xB65F). The other ONLINE members named in MemberNames
// (the member list EXCLUDING the actor, whose answer is the ack) get
// PushPayload, a 0x3B29 subOp-6 delta, or - when Reseed is set - the
// full 0x32C4 guild block of Guild and Members. EVERY refusal stays fully
// silent - deliberately no ErrorPayload slot: the pinned grant codes
// (0x4D/0x52, 0x58-0x5D) live at client-LOCAL call sites, so a server
// trigger->code mapping onto result=2 would be invented.
type grantOutcome struct {
	AckPayload  []byte
	PushPayload []byte
	Reseed      bool
	Guild       enterworld.GuildRecord
	Members     []enterworld.GuildMemberRecord
	MemberNames []string
	TargetName  string
	Refusal     string
}

func refusedGrant(reason string) grantOutcome {
	return grantOutcome{Refusal: reason}
}

// HandleNameGrant applies one decoded 0x72BC request through
// UpdateGuildAs (single-plane member-row update - no FK moves, the
// notice-edit atomic unit): persist the granted title on the target's
// GrantName field, answer 0xB2BC to the actor, and re-seed the other
// online members with the full 0x32C4 guild block.
//
// The v1.150 member delta has no grant-name arm: CIFGuildMember_
// OnMemberUpdated reads mask 0x20 as an ASCII string into the
// member NAME (+0x04), and 0x40 is the fortress role. Sending the title
// there renamed the grantee in every roster ("why is my name Bubu?") and
// broke their own [Guild * title] board, which finds the member by name.
// 0x32C4 carries each member's grant name (+0x40) and the client already
// takes it mid-session (a guild join re-seeds the joiner). Characters in
// view keep the title their spawn row carried (ICUser +0x78C is written
// only by the 0x30D7 parse) until they next come into view, as native.
//
// Port-only, not native: v1.188 hands the grant to the shard manager
// (the v1.188 nickname-update handler) and its push to the other
// members was not found; the earlier port sent subOp-6 &0x20, which v1.150
// applies to the member name. Whatever native sent, the v1.150 delta cannot
// carry a title, so this re-seed is a deliberate deviation that fixes it. Beyond the shared leader gate the
// guild must be level GuildNameGrantMinLevel+ (the pinned client arm),
// and the title must be non-empty and within GrantNameMaxBytes (the
// documented DECISION cap - the native charset walk 0x4D is a
// client-local frontier, not mirrored).
func HandleNameGrant(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) grantOutcome {
	request, err := DecodeNameGrantRequest(payload)
	if err != nil {
		return refusedGrant(err.Error())
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedGrant("characterNotFound")
	}
	if deps.GuildAuthority() == nil {
		return refusedGrant("no guild store wired")
	}
	if request.GrantName == "" {
		return refusedGrant("empty grant name")
	}
	if len(request.GrantName) > GrantNameMaxBytes {
		return refusedGrant(fmt.Sprintf("grant name %d bytes exceeds the %d cap", len(request.GrantName), GrantNameMaxBytes))
	}

	var target enterworld.GuildMemberRecord
	commandRefusal := ""
	snapshot, refusal := deps.GuildAuthority().UpdateGuildAs(
		divisionID,
		actor.ID,
		"guild-name-grant",
		enterworld.GuildAuthorization{LeaderOnly: true},
		func(
			guild enterworld.GuildRecord,
			members []enterworld.GuildMemberRecord,
		) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			if guild.Level < GuildNameGrantMinLevel {
				commandRefusal = fmt.Sprintf(
					"guild level %d below the name-grant gate %d",
					guild.Level,
					GuildNameGrantMinLevel,
				)
				return guild, members, false
			}
			var found bool
			target, found = memberByJID(members, request.TargetJID)
			if !found {
				commandRefusal = fmt.Sprintf("no member with jid %d", request.TargetJID)
				return guild, members, false
			}
			for i := range members {
				if members[i].CharID == target.CharID {
					members[i].GrantName = request.GrantName
				}
			}
			return guild, members, true
		},
	)
	if refusal.Refused() {
		if commandRefusal != "" {
			return refusedGrant(commandRefusal)
		}
		if refusal == enterworld.GuildRefusalLeaderRequired {
			return refusedGrant("non-leader cannot grant")
		}
		return refusedGrant(guildRefusalReason(refusal))
	}
	return grantOutcome{
		AckPayload:  EncodeNameGrantAckB2BC(target.JID, request.GrantName),
		Reseed:      true,
		Guild:       snapshot.Guild,
		Members:     snapshot.Members,
		MemberNames: memberNamesExcept(snapshot.Members, actor.ID),
		TargetName:  target.Name,
	}
}

// ValidFortressRole reports whether role is one of the closed native
// fortress-title values. A switch keeps this policy immutable.
func ValidFortressRole(role uint8) bool {
	switch role {
	case FortressRoleNone,
		FortressRoleCommander,
		FortressRoleDeputy,
		FortressRoleEngineer,
		FortressRoleGuard,
		FortressRoleElite,
		FortressRoleMercenary:
		return true
	default:
		return false
	}
}

// HandlePositionGrant applies one decoded 0x765F request through
// UpdateGuildAs: persist the fortress-role byte on the target's
// FortressRole field, answer 0xB65F to the actor, fan the subOp-6
// &0x40 delta to the other online members. The role byte must sit in
// ValidFortressRole; the shared grantTarget front carries the leader
// gate (position grant has NO guild-level gate - only the name grant's
// arm is level-gated client-side).
func HandlePositionGrant(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) grantOutcome {
	request, err := DecodePositionGrantRequest(payload)
	if err != nil {
		return refusedGrant(err.Error())
	}
	if !ValidFortressRole(request.Position) {
		return refusedGrant(fmt.Sprintf("fortress role %#x outside the pinned domain (0/1/2/4/8/0x10/0x20)", request.Position))
	}
	// The commander is the guild master's own role: the client's grant
	// window offers 0, 2, 4, 8, 0x10 and 0x20 only (client
	// CIFGuildFortressGrant_OnPositionRadio), the retail
	// _Guild_FnAddMember gives it to MemberClass 0, and
	// _Guild_Delegate_Master refuses (-1002) a master without it. A grant
	// can neither hand it out nor take it from the master.
	if request.Position == FortressRoleCommander {
		return refusedGrant("the commander role belongs to the guild master")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedGrant("characterNotFound")
	}
	if deps.GuildAuthority() == nil {
		return refusedGrant("no guild store wired")
	}

	var target enterworld.GuildMemberRecord
	commandRefusal := ""
	snapshot, refusal := deps.GuildAuthority().UpdateGuildAs(
		divisionID,
		actor.ID,
		"guild-position-grant",
		enterworld.GuildAuthorization{LeaderOnly: true},
		func(
			guild enterworld.GuildRecord,
			members []enterworld.GuildMemberRecord,
		) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			var found bool
			target, found = memberByJID(members, request.TargetJID)
			if !found {
				commandRefusal = fmt.Sprintf("no member with jid %d", request.TargetJID)
				return guild, members, false
			}
			if target.Grade == LeaderGrade {
				commandRefusal = "the guild master keeps the commander role"
				return guild, members, false
			}
			for i := range members {
				if members[i].CharID == target.CharID {
					members[i].FortressRole = request.Position
				}
			}
			return guild, members, true
		},
	)
	if refusal.Refused() {
		if commandRefusal != "" {
			return refusedGrant(commandRefusal)
		}
		if refusal == enterworld.GuildRefusalLeaderRequired {
			return refusedGrant("non-leader cannot grant")
		}
		return refusedGrant(guildRefusalReason(refusal))
	}
	return grantOutcome{
		AckPayload:  EncodePositionGrantAckB65F(target.JID, request.Position),
		PushPayload: EncodeMemberFortressRole3B29(target.JID, request.Position),
		MemberNames: memberNamesExcept(snapshot.Members, actor.ID),
		TargetName:  target.Name,
	}
}

// memberByCharID resolves one member row by store character id.
func memberByCharID(members []enterworld.GuildMemberRecord, charID int64) (enterworld.GuildMemberRecord, bool) {
	for _, member := range members {
		if member.CharID == charID {
			return member, true
		}
	}
	return enterworld.GuildMemberRecord{}, false
}

// memberByJID resolves one member row by wire member jid (the id the
// grant composers carry and the grant acks echo).
func memberByJID(members []enterworld.GuildMemberRecord, jid uint32) (enterworld.GuildMemberRecord, bool) {
	for _, member := range members {
		if member.JID == jid {
			return member, true
		}
	}
	return enterworld.GuildMemberRecord{}, false
}

// memberNamesExcept collects the member names for a fan-out that skips
// one character (the grant fan-outs exclude the acting granter - the
// wire.go encoder comments carry the decision).
func memberNamesExcept(members []enterworld.GuildMemberRecord, exceptCharID int64) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		if member.CharID == exceptCharID {
			continue
		}
		names = append(names, member.Name)
	}
	return names
}
