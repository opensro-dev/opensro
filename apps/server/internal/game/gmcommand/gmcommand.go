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
	case SubZoe:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				KillGMZoe(string, string, uint32, uint8) bool
			}); ok && owner.KillGMZoe(divisionID, sender.Name, request.RefObjID, request.Amount) {
				return Outcome{Ack: []byte{AckResultOK, SubZoe}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "zoe-refused"}
	case SubMobKill:
		// sub_751ec0 selects case 5 (no tail) for result 1 and the generic
		// system message for result 2, like every other relayed command.
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				KillGMMonster(string, string, uint32, bool) bool
			}); ok && owner.KillGMMonster(divisionID, sender.Name, request.TargetGID, request.MobKillMode == 0) {
				return Outcome{Ack: []byte{AckResultOK, SubMobKill}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "monster-kill-refused"}
	case SubInitQ, SubResetQ, SubCompQ, SubRemoveQ:
		return handleQuestTool(divisionID, sender, request, status)
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
	case SubGoTown:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				GoTownGM(string, string) bool
			}); ok && len(payload) == 1 && owner.GoTownGM(divisionID, sender.Name) {
				return Outcome{Ack: []byte{AckResultOK, SubGoTown}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "gotown-refused"}
	case SubToTown, SubMoveToUser, SubRecallUser:
		return handlePlayerTeleport(divisionID, sender, request, status)
	case SubMoveToNPC:
		if len(status) > 0 && request.HasName && request.Name != "" {
			if owner, ok := status[0].(interface {
				MoveToNpcGM(string, string, string) bool
			}); ok && owner.MoveToNpcGM(divisionID, sender.Name, request.Name) {
				return Outcome{Ack: []byte{AckResultOK, SubMoveToNPC}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "movetonpc-refused"}
	case SubOpenWorld:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				InstanceGM(string, uint16, uint16) bool
			}); ok && owner.InstanceGM(divisionID, request.WorldID, request.LayerID) {
				return Outcome{Ack: []byte{AckResultOK, SubOpenWorld}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "instance-refused"}
	case SubSiegeStructure:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				SiegeStructureGM(string, string, uint8, uint32, uint32, uint8) bool
			}); ok && owner.SiegeStructureGM(divisionID, sender.Name, request.SiegeAction, request.FortressID, request.EventStructID, request.SiegeParam) {
				return Outcome{Ack: []byte{AckResultOK, SubSiegeStructure}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "siege-structure-refused"}
	case SubSiegePeriod:
		if len(status) > 0 {
			if owner, ok := status[0].(interface {
				SiegePeriodGM(string, uint8) bool
			}); ok && owner.SiegePeriodGM(divisionID, request.SiegeAction) {
				return Outcome{Ack: []byte{AckResultOK, SubSiegePeriod}}
			}
		}
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "siege-period-refused"}
	case SubFlagWorld:
		// 0x0051FCF3..0x0051FD7D (subcmd 0x30): reads u8 flagCmd, sends
		// 0x74B0 [u8(3), u32(flagCmd)] to ShardManager (CFlagWorldMgr Capture
		// the Flag, a v1.188 battle-arena feature outside v1.150), and
		// unconditionally replies [1, 0x30].
		return Outcome{Ack: []byte{AckResultOK, SubFlagWorld}}
	case SubSetTime:
		return handleSetTime(request, status)
	case SubWorldStatus:
		return handleWorldStatus(divisionID, request, status)
	case SubBan:
		return handleBan(deps, presence, divisionID, sender, request, status)
	case SubRecallGuild:
		return handleRecallGuild(divisionID, sender, request, status)
	case SubLieName, SubRealName:
		return handleWho(deps, presence, divisionID, request)
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
	case SubSetSpeed:
		// The port's /SETSPEED (action/gmspeed.go): the ack echoes the percent.
		if len(status) > 0 && status[0] != nil {
			if owner, ok := status[0].(interface {
				SetGMSpeed(string, string, uint16) bool
			}); ok && owner.SetGMSpeed(divisionID, sender.Name, request.SpeedPercent) {
				return Outcome{Ack: wire.NewWriter(4).U8(AckResultOK).U8(SubSetSpeed).U16(request.SpeedPercent).Payload()}
			}
		}
		return Outcome{Ack: []byte{AckResultFail, SubSetSpeed}, Refusal: "set-speed-refused"}
	default:
		// Every command this server cannot honor: the faithful result-2
		// refusal, no state touched.
		return Outcome{
			Refusal: fmt.Sprintf("gmcommand: subcommand 0x%02X not honored by this server", request.Subcmd),
			Ack:     EncodeRequestFailure(request),
		}
	}
}

// PlayerTeleports is the action-owned port for the commands that move a
// named player or move the GM to one (action/gmteleport.go).
type PlayerTeleports interface {
	SendToTownGM(division, name, target string) bool
	MoveToUserGM(division, name, target string) bool
	RecallUserGM(division, name, target string) bool
}

// handlePlayerTeleport honors /TOTOWN, /MOVETOUSER and /RECALLUSER. The
// server's GM handler acks the teleport it performed with [1, subcmd]; an
// empty name, a target not in the world or a refused move is the result-2
// ack.
func handlePlayerTeleport(divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	refusal := Outcome{Ack: EncodeRequestFailure(request), Refusal: fmt.Sprintf("gmcommand: teleport 0x%02X refused", request.Subcmd)}
	if !request.HasName || request.Name == "" || len(status) == 0 {
		return refusal
	}
	owner, ok := status[0].(PlayerTeleports)
	if !ok {
		return refusal
	}
	var moved bool
	switch request.Subcmd {
	case SubToTown:
		moved = owner.SendToTownGM(divisionID, sender.Name, request.Name)
	case SubMoveToUser:
		moved = owner.MoveToUserGM(divisionID, sender.Name, request.Name)
	case SubRecallUser:
		moved = owner.RecallUserGM(divisionID, sender.Name, request.Name)
	}
	if !moved {
		return refusal
	}
	return Outcome{Ack: []byte{AckResultOK, request.Subcmd}}
}

// QuestTools is the action-owned port for the GM's quest commands
// (action/gmquest.go): each acts on the GM's own journal.
type QuestTools interface {
	InitQuestGM(division, name, codename string) bool
	RemoveQuestGM(division, name, codename string) bool
	CompleteQuestGM(division, name, codename string) bool
	ResetQuestsGM(division, name string) bool
}

// handleQuestTool honors /INITQ, /RESETQ, /COMPQ and /REMOVEQ (subs 0x1B,
// 0x1C, 0x1D, 0x1E). 751EC0 selects case 5 (no tail) for result 1 and the
// generic system message for result 2, like every other relayed command. An
// empty codename is a refusal; the owner refuses an unknown or inactive quest.
func handleQuestTool(divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	refusal := Outcome{Ack: EncodeRequestFailure(request), Refusal: fmt.Sprintf("gmcommand: quest tool 0x%02X refused", request.Subcmd)}
	if len(status) == 0 {
		return refusal
	}
	owner, ok := status[0].(QuestTools)
	if !ok {
		return refusal
	}
	var done bool
	switch request.Subcmd {
	case SubResetQ:
		done = owner.ResetQuestsGM(divisionID, sender.Name)
	default:
		if !request.HasName || request.Name == "" {
			return refusal
		}
		switch request.Subcmd {
		case SubInitQ:
			done = owner.InitQuestGM(divisionID, sender.Name, request.Name)
		case SubCompQ:
			done = owner.CompleteQuestGM(divisionID, sender.Name, request.Name)
		case SubRemoveQ:
			done = owner.RemoveQuestGM(divisionID, sender.Name, request.Name)
		}
	}
	if !done {
		return refusal
	}
	return Outcome{Ack: []byte{AckResultOK, request.Subcmd}}
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
