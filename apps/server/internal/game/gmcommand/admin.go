/*
===========================================================================

admin.go - the GM's lookup, kick, status and guild recall commands

/WORLDSTATUS, /BAN, /RECALLGUILD, /LIENAME and /REALNAME: arms of the
v1.188 server's per-player GM special-command handler.
Lookups and the kick are answered here from the character records and
the presence directory; world counts and the guild recall belong to the
action owner and come through its ports.

===========================================================================
*/
package gmcommand

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// noNickname is the server's who-command answer for a character
// whose account name reads empty.
const noNickname = "##DONT_HAVE_NICKNAME##"

/*
================
WorldStatus

The action-owned counts /WORLDSTATUS reports.
================
*/
type WorldStatus interface {
	WorldStatusGM(division string) (players, others, items uint32)
}

/*
================
GameClock

The action-owned /SETTIME.
================
*/
type GameClock interface {
	SetGameTimeGM(hour uint8)
}

/*
================
GuildRecall

The action-owned /RECALLGUILD.
================
*/
type GuildRecall interface {
	RecallGuildGM(division, name, guild string) bool
}

/*
================
SessionEnder

The presence directory's logout of a character, which a kick needs.
================
*/
type SessionEnder interface {
	EndSessionByName(divisionID, characterName string) bool
}

/*
================
BanObserver

The owner told about a completed /BAN (the action runtime, which purges
the website chat feed). The feed matches the character identity recorded
on every line, so lines a suited player left under a job alias go too.
Port-only, not native.
================
*/
type BanObserver interface {
	PlayerBannedGM(divisionID, characterName string)
}

/*
================
handleWorldStatus

The GM handler's case 3: [1, 4, i32 players, i32 other objects, i32 ground items],
the layout the client's NetMsg_OnGmCommandAck_B5B6 (@0x00751ec0) prints
as "Player:%d, NPC_Mob:%d, DroppedItem:%d".
================
*/
func handleWorldStatus(divisionID string, request Request, status []BodyStatusCommands) Outcome {
	if len(status) == 0 {
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "world-status-unavailable"}
	}
	owner, ok := status[0].(WorldStatus)
	if !ok {
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "world-status-unavailable"}
	}
	players, others, items := owner.WorldStatusGM(divisionID)
	return Outcome{Ack: wire.NewWriter(14).U8(AckResultOK).U8(SubWorldStatus).U32(players).U32(others).U32(items).Payload()}
}

/*
================
handleBan

The GM handler's case 0xA: the named player in the world is logged out
(the server's exit with reason 4: the ordinary save-and-leave path)
and the GM gets [1, sub]. Despite its name /BAN bars nothing; the account
stays able to log in. The port ends the target's session normally, so the
departure owner saves and removes them exactly as on a logout, and leaves
an operator log line naming the GM and the player.
================
*/
func handleBan(deps Dependencies, presence PresenceView, divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	refusal := Outcome{Ack: EncodeRequestFailure(request), Refusal: "ban-refused"}
	if !request.HasName || request.Name == "" {
		return refusal
	}
	directory, ok := presence.(SessionEnder)
	if !ok {
		return refusal
	}
	target := characterSnapshot(deps, divisionID, findCharacterByName(deps, divisionID, request.Name))
	if target == nil || target.DeletePending {
		return refusal
	}
	// Port-only: the website's chat feed drops the named player's lines
	// whether or not they are still in the world. Natively /BAN of an
	// offline player is refused and does nothing; the GM's intent to remove
	// them is plain, and a player who spoke and logged off would otherwise
	// stay on the site until newer lines pushed them out.
	if len(status) > 0 {
		if owner, ok := status[0].(BanObserver); ok {
			owner.PlayerBannedGM(divisionID, target.Name)
		}
	}
	if !directory.EndSessionByName(divisionID, target.Name) {
		return refusal
	}
	// The operator trail (#681), as a silk grant leaves one: who removed
	// whom from the world; the log line carries when.
	log.WithFields(log.Fields{"gm": sender.Name, "division": divisionID, "character": target.Name,
		"account": target.AccountID}).Info("gmcommand: /BAN logged a player out")
	return Outcome{Ack: []byte{AckResultOK, SubBan}}
}

/*
================
handleRecallGuild

The GM handler's case 0xE, through the action owner. A missing guild is the
result-2 refusal.
================
*/
func handleRecallGuild(divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	refusal := Outcome{Ack: EncodeRequestFailure(request), Refusal: "recall-guild-refused"}
	if !request.HasName || request.Name == "" || len(status) == 0 {
		return refusal
	}
	owner, ok := status[0].(GuildRecall)
	if !ok || !owner.RecallGuildGM(divisionID, sender.Name, request.Name) {
		return refusal
	}
	return Outcome{Ack: []byte{AckResultOK, SubRecallGuild}}
}

/*
================
handleWho

The server's who command:
  - /LIENAME (0x19) takes an account name and answers the name of that
    account's character in the world (an object-manager lookup by account);
  - /REALNAME (0x1A) takes a character name and answers its account name,
    or noNickname when that reads empty.

Either one finds only a player in the world; a miss is the result-2
answer that repeats the name asked for.
================
*/
func handleWho(deps Dependencies, presence PresenceView, divisionID string, request Request) Outcome {
	refusal := Outcome{Ack: EncodeRequestFailure(request), Refusal: "who-not-found"}
	if !request.HasName || request.Name == "" || presence == nil {
		return refusal
	}
	if request.Subcmd == SubLieName {
		for _, candidate := range deps.CharactersForDivision(divisionID) {
			c := characterSnapshot(deps, divisionID, candidate)
			// The who command's account lookup is an exact match.
			if c == nil || c.DeletePending || c.AccountID != request.Name {
				continue
			}
			if presence.OnlineByName(divisionID, c.Name) {
				return Outcome{Ack: EncodeAckGuide(SubLieName, c.Name)}
			}
		}
		return refusal
	}
	target := characterSnapshot(deps, divisionID, findCharacterByName(deps, divisionID, request.Name))
	if target == nil || target.DeletePending || !presence.OnlineByName(divisionID, target.Name) {
		return refusal
	}
	account := target.AccountID
	if strings.TrimSpace(account) == "" {
		account = noNickname
	}
	return Outcome{Ack: EncodeAckGuide(SubRealName, account)}
}

/*
================
handleSetTime

The GM handler's case 8 relays the hour to the shard manager, which sets the world
clock for every GameWorld and answers the GM itself. The port's one
GameWorld sets it through the action owner. INFERENCE: the GM's ack is
[1, sub], which the client's NetMsg_OnGmCommandAck_B5B6 (@0x00751ec0)
shows nothing for, as for every result-1 code it has no text for.
================
*/
func handleSetTime(request Request, status []BodyStatusCommands) Outcome {
	if len(status) == 0 {
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "set-time-unavailable"}
	}
	owner, ok := status[0].(GameClock)
	if !ok {
		return Outcome{Ack: EncodeRequestFailure(request), Refusal: "set-time-unavailable"}
	}
	owner.SetGameTimeGM(request.Amount)
	return Outcome{Ack: []byte{AckResultOK, SubSetTime}}
}
