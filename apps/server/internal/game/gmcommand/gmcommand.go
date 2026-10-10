package gmcommand

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// PresenceView is the live-peer predicate the FINDUSER arm consults.
type PresenceView interface {
	OnlineByName(divisionID, name string) bool
}

// Outcome is one handled 0x75B6 request. Ack, when non-nil, is the 0xB5B6
// body sent to the requesting session; nil is SILENT. Refusal carries the
// log-only reason.
//
// THE PRIVILEGE RULE: an unprivileged sender's Outcome is {Ack:nil} - the
// command is neither executed nor acknowledged. Client-side Console_IsAllowed
// is NOT security (it is bit0 OR a retail login-session byte, so a non-GM
// passes it and composes the frame), so this server-side GMPrivilege gate is
// the sole authority. The lane NEVER mutates any store/world state for an
// unprivileged sender.
type Outcome struct {
	Ack     []byte
	Refusal string
	// PrivilegeDenied marks a command from a character without GMPrivilege:
	// silent on the wire, but an operator audit event (register.go).
	PrivilegeDenied bool
}

func refused(reason string) Outcome {
	return Outcome{Refusal: reason}
}

// HandleGmCommand routes one decoded 0x75B6 request. Transport-free: the
// register glue owns the Send. The gate order is deliberate:
//
//  1. no bound character / delete-pending -> silent refusal (no ack).
//  2. NOT GMPrivilege -> silent refusal (no ack, no mutation). This is the
//     security boundary; nothing an unprivileged sender composes has any
//     effect.
//  3. privileged -> dispatch FINDUSER or the action-owned body toggle port.
//     Commands without an implemented authority remain refused.
func HandleGmCommand(deps Dependencies, presence PresenceView, divisionID string, sender *enterworld.Character, payload []byte, status ...BodyStatusCommands) Outcome {
	if sender == nil {
		return refused("characterNotFound")
	}
	sender = characterSnapshot(deps, divisionID, sender)
	if sender == nil {
		return refused("characterNotFound")
	}
	if sender.DeletePending {
		return refused("deletePending")
	}
	if !sender.GMPrivilege {
		// The privilege boundary: refuse SILENTLY and do nothing. A
		// non-GM cannot distinguish this from the command not existing,
		// and no retail refusal bytes for a privilege denial are pinned.
		denied := refused(fmt.Sprintf("gmcommand: 0x75B6 from non-GM %s refused (no privilege)", sender.Name))
		denied.PrivilegeDenied = true
		return denied
	}
	request, err := DecodeGmCommand(payload)
	if err != nil {
		return refused(err.Error())
	}

	switch request.Subcmd {
	case SubWarp:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				WarpGM(string, string, wire.Position) bool
			}); ok && owner.WarpGM(divisionID, sender.Name, request.Destination) {
				return Outcome{Ack: []byte{AckResultOK, SubWarp}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "warp-refused"}
	case SubMakeItem:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				MakeGMItem(string, string, uint32, uint8) bool
			}); ok && owner.MakeGMItem(divisionID, sender.Name, request.RefObjID, request.Amount) {
				return Outcome{Ack: []byte{AckResultOK, SubMakeItem}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "item-creation-refused"}
	case SubLoadMonster:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				LoadGMMonsters(string, string, uint32, uint8, uint8) bool
			}); ok && owner.LoadGMMonsters(divisionID, sender.Name, request.RefObjID, request.Amount, request.MonsterType) {
				return Outcome{Ack: []byte{AckResultOK, SubLoadMonster}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "monster-load-refused"}
	case SubInvisible, SubInvincible:
		if len(payload) != 1 || len(status) == 0 || status[0] == nil {
			return Outcome{Ack: EncodeRequestFailure(request), Refusal: "body-status-unavailable"}
		}
		value := uint8(4)
		if request.Subcmd == SubInvincible {
			value = 3
		}
		if !status[0].ToggleGMBodyStatus(divisionID, sender.Name, value) {
			return Outcome{Ack: EncodeRequestFailure(request), Refusal: "body-status-refused"}
		}
		// 751EC0: both subcommands select case 5 (no success tail).
		return Outcome{Ack: []byte{AckResultOK, request.Subcmd}}
	case SubFindUser:
		return handleFindUser(deps, presence, divisionID, sender, request)
	case SubGrantSilk:
		// Operator tooling (action/operator_silk.go): the ack carries the
		// recipient's new balance.
		if len(status) > 0 && request.Name != "" {
			if owner, ok := status[0].(interface {
				GrantGMSilk(string, string, string, uint32) (uint32, bool)
			}); ok {
				if balance, granted := owner.GrantGMSilk(divisionID, sender.Name, request.Name, request.Silk); granted {
					return Outcome{Ack: wire.NewWriter(6).U8(AckResultOK).U8(SubGrantSilk).U32(balance).Payload()}
				}
			}
		}
		return Outcome{Ack: []byte{AckResultFail, SubGrantSilk}, Refusal: "silk-grant-refused"}
	default:
		// Every command this server cannot honor: the faithful result-2
		// refusal, no state touched.
		return Outcome{
			Refusal: fmt.Sprintf("gmcommand: subcommand 0x%02X not honored by this server", request.Subcmd),
			Ack:     EncodeRequestFailure(request),
		}
	}
}

// BodyStatusCommands is the action-owner mutation port. The dispatcher owns no
// status fields, effect timers, combat state or publication queues.
type BodyStatusCommands interface {
	ToggleGMBodyStatus(divisionID, characterName string, requested uint8) bool
}

// handleFindUser honors /FINDUSER (subcmd 0x01): resolve the target name in
// the sender's division and answer with the result-1 guide message the
// client's sub_751ec0 case-0 arm presents. Read-only - it mutates nothing.
// A target that is unknown or offline is refused with the result-2 ack (the
// native FINDUSER-fail leg, generic subcmd-1 system message).
func handleFindUser(deps Dependencies, presence PresenceView, divisionID string, sender *enterworld.Character, request Request) Outcome {
	if !request.HasName || request.Name == "" {
		return Outcome{
			Refusal: "gmcommand: /FINDUSER with an empty name",
			Ack:     EncodeAckFail(SubFindUser),
		}
	}
	target := findCharacterByName(deps, divisionID, request.Name)
	target = characterSnapshot(deps, divisionID, target)
	if target == nil || target.DeletePending ||
		presence == nil || !presence.OnlineByName(divisionID, target.Name) {
		return Outcome{
			Refusal: fmt.Sprintf("gmcommand: /FINDUSER target %q not online in division %s", request.Name, divisionID),
			Ack:     EncodeAckFail(SubFindUser),
		}
	}
	return Outcome{Ack: EncodeAckGuide(SubFindUser, findUserGuideText(target))}
}

// findUserGuideText builds the guide string the client shows for a located
// user: the stored name plus, when a spawn pose is persisted, its region id.
// This is the only server-authored content in the lane; it reports real
// state (name + region) and nothing invented.
func findUserGuideText(target *enterworld.Character) string {
	if target.World != nil && target.World.Spawn != nil && target.World.Spawn.RegionID != nil {
		return fmt.Sprintf("%s (region 0x%04X)", target.Name, uint16(*target.World.Spawn.RegionID))
	}
	return target.Name
}

// findCharacterByName resolves a division character record by name,
// case-insensitively - the chat lane's lookup, mirrored (theirs is
// unexported): CreateCharacter refuses case-insensitive duplicates, so the
// fold is unambiguous.
func findCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

// characterSnapshot copies mutable character state while the authority read
// door is held. Privilege and world-location decisions must never inspect a
// live record outside the authority boundary.
func characterSnapshot(
	deps Dependencies,
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if deps == nil || character == nil {
		return nil
	}
	var snapshot *enterworld.Character
	deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	return snapshot
}
