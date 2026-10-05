/*
===========================================================================

operations_permission.go - the guild master grants member rights

CIFGuildGrantPower's apply (5EE410) sends 0x744E [u8 count] and a [u32
member jid][u32 rights] pair per changed member
(NetClient_SendGuildPermissionUpdate744E 5EE1C0); the v1.188 handler is
CGObjPC_HandleGuildUpdatePermission7104 (5173E0). Only the master grants
(GuildManager_RequestPermissionUpdate 5C69F0: 0x4C1E), and no more than
twelve members may hold the union chat right afterwards
(CGuild_UnionChatWithinLimit 5C49A0: 0x4C4B). A refusal answers 0xB44E
[2][code] (CPSMission_OnGuildPermissionResult0xB44E 75CB80); success
sends no answer, only the 0x3B29 0x16 batch of the new rights (mask 0x10)
to every online member.

INFERENCE: 5C69F0 also refuses removing the storage right from the
member using the guild storage (0x4C4C); no guild storage is ported, so
that test always admits. The master's own rights are not edited: the
leader holds them all.

===========================================================================
*/
package guild

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	OpGuildPermissionRequest uint16 = 0x744E
	OpGuildPermissionResult  uint16 = 0xB44E

	guildUpdateMemberBatch uint8 = 0x16
	memberDeltaPermissions uint8 = 0x10

	// unionChatMemberLimit is 5C49A0's `count <= 0xC`.
	unionChatMemberLimit = 12

	permErrUnionChatFull uint8 = 0x4B
)

/*
================
PermissionGrant
================
*/
type PermissionGrant struct {
	JID      uint32
	PermMask uint32
}

/*
================
DecodePermissionRequest
================
*/
func DecodePermissionRequest(payload []byte) ([]PermissionGrant, error) {
	r := wire.NewReader(payload)
	count, err := r.U8()
	if err != nil {
		return nil, err
	}
	grants := make([]PermissionGrant, 0, count)
	for range count {
		jid, err := r.U32()
		if err != nil {
			return nil, err
		}
		mask, err := r.U32()
		if err != nil {
			return nil, err
		}
		grants = append(grants, PermissionGrant{JID: jid, PermMask: mask})
	}
	return grants, r.Done()
}

/*
================
EncodeMemberPermissions3B29
================
*/
func EncodeMemberPermissions3B29(grants []PermissionGrant) []byte {
	w := wire.NewWriter(3 + 8*len(grants))
	w.U8(guildUpdateMemberBatch).U8(uint8(len(grants))).U8(memberDeltaPermissions)
	for _, grant := range grants {
		w.U32(grant.JID).U32(grant.PermMask)
	}
	return w.Payload()
}

/*
================
permissionOutcome
================
*/
type permissionOutcome struct {
	ErrorPayload []byte
	PushPayload  []byte
	MemberNames  []string
	Refusal      string
}

/*
================
HandlePermissionUpdate
================
*/
func HandlePermissionUpdate(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) permissionOutcome {
	grants, err := DecodePermissionRequest(payload)
	if err != nil {
		return permissionOutcome{Refusal: err.Error()}
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil || deps.GuildAuthority() == nil {
		return permissionOutcome{Refusal: "characterNotFound"}
	}
	var applied []PermissionGrant
	code := uint8(0)
	snapshot, refusal := deps.GuildAuthority().UpdateGuildAs(
		divisionID,
		actor.ID,
		"guild-permission",
		enterworld.GuildAuthorization{LeaderOnly: true},
		func(
			guild enterworld.GuildRecord,
			members []enterworld.GuildMemberRecord,
		) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			applied = applied[:0]
			for _, grant := range grants {
				for i := range members {
					if members[i].JID == grant.JID && members[i].Grade != 0 && members[i].PermMask != grant.PermMask {
						members[i].PermMask = grant.PermMask
						applied = append(applied, grant)
					}
				}
			}
			holders := 0
			for _, member := range members {
				if member.Grade != 0 && member.PermMask&PermMaskUnionChat != 0 {
					holders++
				}
			}
			if holders > unionChatMemberLimit {
				code = permErrUnionChatFull
				return guild, members, false
			}
			return guild, members, len(applied) != 0
		},
	)
	switch {
	case code != 0:
		return permissionOutcome{Refusal: "union chat rights full", ErrorPayload: EncodeGuildErrorResult(code)}
	case refusal == enterworld.GuildRefusalLeaderRequired:
		return permissionOutcome{Refusal: "only the master grants", ErrorPayload: EncodeGuildErrorResult(unionErrPermission)}
	case refusal == enterworld.GuildRefusalUpdateRejected:
		return permissionOutcome{Refusal: "no right changed"}
	case refusal.Refused():
		return permissionOutcome{Refusal: guildRefusalReason(refusal), ErrorPayload: EncodeGuildErrorResult(unionErrNoGuild)}
	}
	names := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		names = append(names, member.Name)
	}
	return permissionOutcome{
		PushPayload: EncodeMemberPermissions3B29(applied),
		MemberNames: names,
	}
}
