/*
===========================================================================

chat.go - chat routing, native message composition and beta scope.

===========================================================================
*/
package chat

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/party"
)

// ClosedBetaGlobalChat sends ordinary chat across the shard during beta.
// Set false to restore visibility-scoped retail All/GM chat.
const ClosedBetaGlobalChat = true

// ChatTypeGlobal is the native named broadcast channel (client 0x753848).
const ChatTypeGlobal uint8 = 6

// PresenceView is the live-peer predicate the whisper arm consults.
type PresenceView interface {
	OnlineByName(divisionID, name string) bool
}

// PartyView is the party-membership lookup the party arm consults -
// *party.Registry satisfies it.
type PartyView interface {
	PartyOf(divisionID, name string) (party.Snapshot, bool)
}

// Delivery is one presence-targeted 0x3667 send. The register glue resolves
// TargetName through the shared live-session directory and drops it silently
// when the target went offline in between.
/*
================
Delivery
================
*/
type Delivery struct {
	TargetName string
	Payload    []byte
}

// Outcome is one handled 0x7367 request. Ack is the 0xB367 body for the
// sending session (nil = SILENT, the lane's decode-refusal posture -
// only a non-retail client can compose a frame this lane refuses to
// answer). Broadcast, when non-nil, is the 0x3667 body for the sender's
// division cohort EXCLUDING the sender; Deliveries are targeted member/
// whisper sends. Refusal carries the log-only reason.
/*
================
Outcome
================
*/
type Outcome struct {
	Ack        []byte
	Refusal    string
	Broadcast  []byte
	Deliveries []Delivery
}

/*
================
refusedChat
================
*/
func refusedChat(reason string) Outcome {
	return Outcome{Refusal: reason}
}

// HandleChat routes one decoded 0x7367 request. Transport-free: the
// register glue owns every Send/Broadcast.
//
// Beta scope is selected once below. The transport uses observed-object
// delivery when disabled, so local GID-authored chat cannot escape visibility.
//
// NOT built, on purpose:
//   - the cross-shard whisper forward (the dump's offline arm): no
//     second shard exists; offline acks ChatErrCantFindTarget.
//   - a GM bypass of the whisper block: the dump has none and the
//     v1.150 client shows none.
//   - a chat rate gate (the dump's vt+0x638): the transport already
//     rate-limits inbound frames per session (hub ratelimit) and the
//     retail client gates its own 0x7367 sends (CanSendOpcode,
//     sub_71fba0 direct-imported in the sub_6aebd0 fold), so a third
//     cap is speculative; ChatErr 0x0D is never composed.
//   - an abuse/ban-word filter: retail's is config-loaded; no config
//     exists here.
/*
================
HandleChat
================
*/
func HandleChat(deps Dependencies, presence PresenceView, parties PartyView, divisionID string, sender *enterworld.Character, payload []byte) Outcome {
	if sender == nil {
		return refusedChat("characterNotFound")
	}
	sender = characterSnapshot(deps, divisionID, sender)
	if sender == nil {
		return refusedChat("characterNotFound")
	}
	if sender.DeletePending {
		return refusedChat("deletePending")
	}
	request, err := DecodeChatRequest(payload)
	if err != nil {
		return refusedChat(err.Error())
	}

	switch request.ChatType {
	case ChatTypeAll, ChatTypeGM:
		return handleAllChat(request, sender, ClosedBetaGlobalChat)
	case ChatTypeWhisper:
		return handleWhisper(deps, presence, divisionID, sender, request)
	case ChatTypeParty:
		return handlePartyChat(parties, divisionID, sender, request)
	case ChatTypeGuild, ChatTypeUnion:
		return handleGuildUnionChat(deps, divisionID, sender, request)
	default:
		// Only 1/2/3/4/5/0x0B are composable by the retail prefix
		// switch; anything else (a crafted notice 7, stall 9, academy
		// 0x10, ...) acks UIIT_CHATERR_INVALID_COMMAND - never
		// broadcast, so a forged type-7 frame cannot drive the notice
		// banner.
		return Outcome{
			Refusal: fmt.Sprintf("chat type 0x%02X not client-composable", request.ChatType),
			Ack:     EncodeChatAckError(ChatErrInvalidCommand, request.ChatType, request.Second),
		}
	}
}

// handleAllChat is the shared All/GM arm (the v1.188 sub_4b1750 shape:
// requested types 1 and 3 land in one arm). The BROADCAST type byte is
// server-forced: 3 for a privileged speaker (vt+0x53c()==1 in the dump;
// our GMPrivilege flag - the same bit the client reads into
// CICPlayer+0x1890), 1 for everyone else - a non-GM requesting type 3
// broadcasts as plain All. The ACK echoes the REQUESTED type untouched
// (the client's pending-record pop keys on it).
/*
================
handleAllChat
================
*/
func handleAllChat(request Request, sender *enterworld.Character, global bool) Outcome {
	if global {
		return Outcome{Ack: EncodeChatAckSuccess(request.ChatType, request.Second), Broadcast: EncodeChatBroadcastNamed(ChatTypeGlobal, sender.Name, request.Message)}
	}

	broadcastType := ChatTypeAll
	if sender.GMPrivilege {
		broadcastType = ChatTypeGM
	}
	return Outcome{
		Ack:       EncodeChatAckSuccess(request.ChatType, request.Second),
		Broadcast: EncodeChatBroadcastGid(broadcastType, enterworld.ObjectIDForCharacter(sender), request.Message),
	}
}

// handleWhisper is the whisper arm: resolve the target name to an online
// division character, consult the TARGET's blocked-whisperer list, and
// deliver the 0x3667 type-2 line - or don't.
//
// THE PRIVACY RULE (v1.188 sub_4b1750, deliberate): a blocked whisper
// acks SUCCESS to the sender and delivers NOTHING - the sender cannot
// distinguish blocked from delivered. There is no GM bypass.
//
// The block match folds case (strings.EqualFold), CONSISTENT WITH OUR
// REGISTER PATH (whisperblock.go): names are CI-unique (CreateCharacter
// refuses case-insensitive duplicates) and every name path here folds
// (presence keys, party keys, the friend/letter lookups), so a list
// carrying "Berk" blocks a whisper from "berk" - the same documented
// deviation from native's case-sensitive CompareStringA the register
// path already made; a sensitive compare here would let the identical
// character through on a casing technicality.
/*
================
handleWhisper
================
*/
func handleWhisper(deps Dependencies, presence PresenceView, divisionID string, sender *enterworld.Character, request Request) Outcome {
	if request.TargetName == "" {
		return Outcome{
			Refusal: "whisper target empty",
			Ack:     EncodeChatAckError(ChatErrCantFindTarget, request.ChatType, request.Second),
		}
	}
	if strings.EqualFold(request.TargetName, sender.Name) {
		// Self-whisper: success with NO delivery (the dump's self arm).
		// The ack presents the "To [name]" line locally; a delivered
		// copy would be dropped by the client's own-name compare anyway.
		return Outcome{Ack: EncodeChatAckSuccess(request.ChatType, request.Second)}
	}
	target := findCharacterByName(deps, divisionID, request.TargetName)
	target = characterSnapshot(deps, divisionID, target)
	if target == nil || target.DeletePending ||
		presence == nil || !presence.OnlineByName(divisionID, target.Name) {
		// Nonexistent AND offline share ChatErrCantFindTarget: the
		// dump's offline arm is the cross-shard forward we have no
		// equivalent for, and its terminal failure is the same code 3
		// ("Cannot find [%s].").
		return Outcome{
			Refusal: fmt.Sprintf("whisper target %q not online in division %s", request.TargetName, divisionID),
			Ack:     EncodeChatAckError(ChatErrCantFindTarget, request.ChatType, request.Second),
		}
	}
	for _, blockedName := range target.BlockedWhisperers {
		if strings.EqualFold(blockedName, sender.Name) {
			return Outcome{
				Refusal: fmt.Sprintf("whisper from %s to %s suppressed by the target's block list", sender.Name, target.Name),
				Ack:     EncodeChatAckSuccess(request.ChatType, request.Second),
			}
		}
	}
	return Outcome{
		Ack: EncodeChatAckSuccess(request.ChatType, request.Second),
		Deliveries: []Delivery{{
			TargetName: target.Name,
			Payload:    EncodeChatBroadcastNamed(ChatTypeWhisper, sender.Name, request.Message),
		}},
	}
}

// handlePartyChat fans the type-4 line to the sender's party members
// (presence-targeted; the sender's own line came back on the ack). No
// party acks ChatErrNotPartyMember (the dump's 0x200A low byte).
/*
================
handlePartyChat
================
*/
func handlePartyChat(parties PartyView, divisionID string, sender *enterworld.Character, request Request) Outcome {
	var snapshot party.Snapshot
	inParty := false
	if parties != nil {
		snapshot, inParty = parties.PartyOf(divisionID, sender.Name)
	}
	if !inParty {
		return Outcome{
			Refusal: fmt.Sprintf("%s is not in a party", sender.Name),
			Ack:     EncodeChatAckError(ChatErrNotPartyMember, request.ChatType, request.Second),
		}
	}
	payload := EncodeChatBroadcastNamed(ChatTypeParty, sender.Name, request.Message)
	outcome := Outcome{Ack: EncodeChatAckSuccess(request.ChatType, request.Second)}
	for _, member := range snapshot.Members {
		if strings.EqualFold(member.Name, sender.Name) {
			continue
		}
		outcome.Deliveries = append(outcome.Deliveries, Delivery{TargetName: member.Name, Payload: payload})
	}
	return outcome
}

// handleGuildUnionChat is the shared guild/union arm. Guild chat (type
// 5) fans to the stored guild's members; no guild acks ChatErrNoGuild
// (0x0B). Union chat (type 0x0B) resolves the same membership gate, then
// ALWAYS acks ChatErrNoUnion (0x0C): no guild-alliance machinery exists
// on this server, so no character holds union permission - the same
// UIIT_CHATERR_ALLIANCE_PERMISSION_DENIED line either way.
/*
================
handleGuildUnionChat
================
*/
func handleGuildUnionChat(deps Dependencies, divisionID string, sender *enterworld.Character, request Request) Outcome {
	guildID := int64(0)
	inGuild := false
	guilds := deps.GuildAuthority()
	if guilds != nil {
		guildID, inGuild = guilds.GuildOfCharacter(divisionID, sender.ID)
	}
	if !inGuild {
		return Outcome{
			Refusal: fmt.Sprintf("%s is not in a guild", sender.Name),
			Ack:     EncodeChatAckError(ChatErrNoGuild, request.ChatType, request.Second),
		}
	}
	if request.ChatType == ChatTypeUnion {
		return Outcome{
			Refusal: fmt.Sprintf("%s's guild is not in a union (no alliance machinery)", sender.Name),
			Ack:     EncodeChatAckError(ChatErrNoUnion, request.ChatType, request.Second),
		}
	}
	_, members, ok := guilds.Guild(divisionID, guildID)
	if !ok {
		return Outcome{
			Refusal: fmt.Sprintf("guild %d of %s vanished between lookup and read", guildID, sender.Name),
			Ack:     EncodeChatAckError(ChatErrNoGuild, request.ChatType, request.Second),
		}
	}
	payload := EncodeChatBroadcastNamed(ChatTypeGuild, sender.Name, request.Message)
	outcome := Outcome{Ack: EncodeChatAckSuccess(request.ChatType, request.Second)}
	for _, member := range members {
		if member.CharID == sender.ID {
			continue
		}
		outcome.Deliveries = append(outcome.Deliveries, Delivery{TargetName: member.Name, Payload: payload})
	}
	return outcome
}

// findCharacterByName resolves a division character record by name,
// case-insensitively - the community lane's lookup, mirrored (theirs is
// unexported): CreateCharacter refuses case-insensitive duplicates, so
// the fold is unambiguous.
/*
================
findCharacterByName
================
*/
func findCharacterByName(deps Dependencies, divisionID, name string) *enterworld.Character {
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

// characterSnapshot copies mutable character state while the authority read
// door is held. Chat decisions must never inspect the live record after the
// door closes.
/*
================
characterSnapshot
================
*/
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
