/*
===========================================================================

moderation.go - the GM's in-game moderation commands

Port-only, not native. /MUTE and /UNMUTE put a character in or out of the
chat mute book (game/moderation), the same record the operations
console writes, so the muted player sees the client's own "Chat
restricted by GM" notice. /SHADOWMUTE and /UNSHADOWMUTE edit the staff's
shadow-mute list (social/shadowmute): the account keeps chatting and
nobody receives it. /BLOCKLOGOUT and /ALLOWLOGOUT hold a character in the
world by refusing its logout and restart requests (action/departure.go).

The original client knows the two logout words but composes no frame for
them; it has no mute words at all. The port's client sends all six as
subcommands 0xF5..0xFA (wire.go).

The owner's call (2026-11-23): /SHADOWMUTE and /UNSHADOWMUTE are always
on. /MUTE, /UNMUTE, /BLOCKLOGOUT and /ALLOWLOGOUT answer only while
SRO_GM_MODERATION is on; off is native, where no GM command does any of
this.

Every command refuses a GM target, and every success leaves an operator
log line naming the GM, the target and the account.

===========================================================================
*/
package gmcommand

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/moderation"
	"opensro.online/server/internal/game/social/shadowmute"
)

// EnvGMModeration turns /MUTE, /UNMUTE, /BLOCKLOGOUT and /ALLOWLOGOUT on
// ("on", "1", "true"). Default off.
const EnvGMModeration = "SRO_GM_MODERATION"

// muteTimeLayout prints a mute's end in the GM's reply; the book keeps
// every stamp in UTC.
const muteTimeLayout = "2006-01-02 15:04 UTC"

/*
================
LogoutBlocks

The departure owner's hold on a character's logout (action/departure.go).
SetLogoutBlockGM reports whether the hold changed.
================
*/
type LogoutBlocks interface {
	SetLogoutBlockGM(division, name string, blocked bool) bool
}

/*
================
Moderation

What the moderation commands act on. Enabled is SRO_GM_MODERATION; a nil
member refuses its commands.
================
*/
type Moderation struct {
	Enabled     bool
	Mutes       *moderation.Book
	ShadowMutes *shadowmute.List
	Logout      LogoutBlocks
	Now         func() time.Time
}

/*
================
ModerationDesk

The GM lane's owner hands out the shard's Moderation through this port
(the GameWorld wiring composes it beside the action runtime).
================
*/
type ModerationDesk interface {
	GMModeration() *Moderation
}

/*
================
GMModerationFromEnv
================
*/
func GMModerationFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGMModeration))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
handleModeration

Resolves the target, refuses a GM, then acts. Both results carry a text
the GM console prints (EncodeModerationReply).
================
*/
func handleModeration(deps Dependencies, divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	fail := func(text string) Outcome {
		return Outcome{Ack: EncodeModerationReply(AckResultFail, request.Subcmd, text), Refusal: "moderation: " + text}
	}
	var desk *Moderation
	if len(status) > 0 {
		if owner, ok := status[0].(ModerationDesk); ok {
			desk = owner.GMModeration()
		}
	}
	if desk == nil {
		return fail("moderation is not set up on this server")
	}
	if request.Subcmd != SubShadowMute && request.Subcmd != SubShadowUnmute && !desk.Enabled {
		return fail("this command is off on this server (" + EnvGMModeration + ")")
	}
	if !request.HasName || request.Name == "" {
		return fail("name a character")
	}
	target := characterSnapshot(deps, divisionID, findCharacterByName(deps, divisionID, request.Name))
	if target == nil || target.DeletePending {
		return fail("no character named " + request.Name)
	}
	if target.GMPrivilege {
		return fail(target.Name + " is a GM")
	}
	now := time.Now
	if desk.Now != nil {
		now = desk.Now
	}
	text, err := applyModeration(desk, divisionID, sender.Name, target, request, now())
	if err != nil {
		return fail(err.Error())
	}
	log.WithFields(log.Fields{"gm": sender.Name, "division": divisionID, "character": target.Name,
		"account": target.AccountID, "subcmd": fmt.Sprintf("0x%02X", request.Subcmd)}).Info("gmcommand: " + text)
	return Outcome{Ack: EncodeModerationReply(AckResultOK, request.Subcmd, text)}
}

/*
================
applyModeration

One command's effect. The returned text is the GM's reply and the audit
line; an error's text is the refusal the GM reads.
================
*/
func applyModeration(desk *Moderation, divisionID, gm string, target *enterworld.Character, request Request, now time.Time) (string, error) {
	switch request.Subcmd {
	case SubChatMute:
		if desk.Mutes == nil {
			return "", errors.New("the chat mute book is not open")
		}
		mute, err := desk.Mutes.Mute(moderation.Mute{Division: divisionID, Character: target.Name, Operator: "gm:" + gm,
			Clause: request.Clause, Reason: request.Text}, time.Duration(request.Minutes)*time.Minute)
		if err != nil {
			return "", errors.New(strings.TrimPrefix(err.Error(), "moderation: "))
		}
		return target.Name + " muted until " + time.UnixMilli(mute.UntilMs).UTC().Format(muteTimeLayout), nil
	case SubChatUnmute:
		if desk.Mutes == nil {
			return "", errors.New("the chat mute book is not open")
		}
		if _, err := desk.Mutes.Unmute(divisionID, target.Name); err != nil {
			if errors.Is(err, moderation.ErrNoMute) {
				return "", errors.New(target.Name + " is not muted")
			}
			return "", err
		}
		return target.Name + " unmuted", nil
	case SubShadowMute:
		if desk.ShadowMutes == nil {
			return "", errors.New("the shadow-mute list is not open")
		}
		note := fmt.Sprintf("%s by GM %s at %s", target.Name, gm, now.UTC().Format(muteTimeLayout))
		if request.Text != "" {
			note += ": " + request.Text
		}
		added, err := desk.ShadowMutes.Add(target.AccountID, note)
		if err != nil {
			return "", err
		}
		if !added {
			return "", errors.New(target.Name + "'s account is already shadow-muted")
		}
		return target.Name + "'s account shadow-muted", nil
	case SubShadowUnmute:
		if desk.ShadowMutes == nil {
			return "", errors.New("the shadow-mute list is not open")
		}
		removed, err := desk.ShadowMutes.Remove(target.AccountID)
		if err != nil {
			return "", err
		}
		if !removed {
			return "", errors.New(target.Name + "'s account is not shadow-muted")
		}
		return target.Name + "'s account no longer shadow-muted", nil
	case SubBlockLogout, SubAllowLogout:
		if desk.Logout == nil {
			return "", errors.New("logout blocks are not available")
		}
		blocked := request.Subcmd == SubBlockLogout
		if !desk.Logout.SetLogoutBlockGM(divisionID, target.Name, blocked) {
			if blocked {
				return "", errors.New(target.Name + "'s logout is already blocked")
			}
			return "", errors.New(target.Name + "'s logout is not blocked")
		}
		if blocked {
			return target.Name + " cannot log out", nil
		}
		return target.Name + " can log out again", nil
	}
	return "", fmt.Errorf("unknown moderation command 0x%02X", request.Subcmd)
}
