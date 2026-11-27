/*
===========================================================================

groupletter.go - the multi-letter send (0x7600) and its 0xB600 answer

The letter window's guild and union modes send one body to a list of
named recipients. Native shape, read from the v1.150 client's
composer and the reference server's group handler: a chat-blocked
sender is dropped at the door (RegisterLetter); the sender must
stand in a guild (refusal 0x16), the guild must be level 3 for a
guild letter or level 4 for a union letter (0x15), and any other
kind byte is refused (0x05). A recipient name the text filter
rejects ends the whole request silently, as does a filtered body. A
guild letter keeps only the names that are members of the sender's
own guild (case-insensitive); a union letter keeps every name. An
empty name, or a set the filter leaves empty, is refused (0x03);
the body must be 1..1000 bytes (0x10) and is stored clamped to the
mailbox column cap the single letter already writes under. Each
kept name resolves to its character and takes the single letter's
mailbox delivery; a full mailbox names that recipient in the
result-1 answer's failed list, an unknown or unavailable one is
skipped without a word (the native store joins by name and reports
only full boxes). Success and partial success both answer result 1.

===========================================================================
*/

package community

import (
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/textguard"
)

// GroupLetterDelivery is one delivered group letter: the recipient's
// canonical name and the 0x3F9A case-8 push body for their live
// session when one exists (the hub adapter resolves it).
type GroupLetterDelivery struct {
	RecipientName string
	PushPayload   []byte
}

// GroupLetterSendOutcome is one handled 0x7600 request. AckPayload
// is the 0xB600 body for the SENDER - the result-1 failed list on
// delivery, or the result-2 refusal body when the refusal is the
// sender's to see; a refusal the client's own gates already report
// (a filtered name or body) leaves AckPayload nil and the wire
// silent. Deliveries ride along for the adapter's live pushes.
type GroupLetterSendOutcome struct {
	AckPayload []byte
	Deliveries []GroupLetterDelivery
	Refusal    string
}

func refusedGroupLetterSend(reason string) GroupLetterSendOutcome {
	return GroupLetterSendOutcome{Refusal: reason}
}

func refusedGroupLetterSendAnswered(reason string, code uint8) GroupLetterSendOutcome {
	outcome := refusedGroupLetterSend(reason)
	outcome.AckPayload = EncodeGroupLetterSendRefusalB600(code)
	return outcome
}

/*
================
HandleGroupLetterSend

Applies one decoded 0x7600 request in the native order (see the
file banner): guild standing, kind and level, the name filter and
membership pass, the body filter and cap, then one mailbox delivery
per kept name through the single letter's door.
================
*/
func HandleGroupLetterSend(deps Dependencies, divisionID string, sender *enterworld.Character, payload []byte, now time.Time) GroupLetterSendOutcome {
	if sender == nil {
		return refusedGroupLetterSend("characterNotFound")
	}
	letters := deps.LetterAuthority()
	if letters == nil {
		return refusedGroupLetterSend("no letter store wired")
	}
	guilds := deps.GuildAuthority()
	if guilds == nil {
		return refusedGroupLetterSend("no guild store wired")
	}
	request, err := DecodeGroupLetterSendRequest(payload)
	if err != nil {
		return refusedGroupLetterSend(err.Error())
	}

	guildID, member := guilds.GuildOfCharacter(divisionID, sender.ID)
	if !member {
		return refusedGroupLetterSendAnswered("sender stands in no guild", GroupLetterRefusalNoGuild)
	}
	guild, members, found := guilds.Guild(divisionID, guildID)
	if !found {
		return refusedGroupLetterSendAnswered("sender's guild record is gone", GroupLetterRefusalNoGuild)
	}
	switch request.Kind {
	case GroupLetterKindGuild:
		if guild.Level < 3 {
			return refusedGroupLetterSendAnswered(fmt.Sprintf("guild level %d below 3 for a guild letter", guild.Level), GroupLetterRefusalGuildLevel)
		}
	case GroupLetterKindUnion:
		if guild.Level < 4 {
			return refusedGroupLetterSendAnswered(fmt.Sprintf("guild level %d below 4 for a union letter", guild.Level), GroupLetterRefusalGuildLevel)
		}
	default:
		return refusedGroupLetterSendAnswered(fmt.Sprintf("kind %d is not a letter kind", request.Kind), GroupLetterRefusalKind)
	}

	// The name pass: a rejected name ends the request silently; a
	// guild letter keeps only the sender's own guild members.
	type recipient struct {
		requested string
		charID    int64
	}
	kept := make([]recipient, 0, len(request.Names))
	for _, name := range request.Names {
		if textguard.Rejected([]byte(name)) {
			return refusedGroupLetterSend("a recipient name failed the text filter")
		}
		if request.Kind == GroupLetterKindGuild {
			matched := false
			for _, m := range members {
				if strings.EqualFold(m.Name, name) {
					kept = append(kept, recipient{requested: name, charID: m.CharID})
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		} else {
			kept = append(kept, recipient{requested: name, charID: 0})
		}
	}
	if len(kept) == 0 {
		return refusedGroupLetterSendAnswered("no recipient survived the membership filter", GroupLetterRefusalEmpty)
	}
	for _, rcpt := range kept {
		if rcpt.requested == "" {
			return refusedGroupLetterSendAnswered("a recipient name is empty", GroupLetterRefusalEmpty)
		}
	}
	if textguard.Rejected([]byte(request.Body)) {
		return refusedGroupLetterSend("the body failed the text filter")
	}
	if len(request.Body) == 0 || len(request.Body) > GroupLetterBodyMaxBytes {
		return refusedGroupLetterSendAnswered(fmt.Sprintf("body %d bytes outside 1..%d", len(request.Body), GroupLetterBodyMaxBytes), GroupLetterRefusalBodyLength)
	}

	// Resolve union names to characters (guild names already carry
	// their member's id) and take one snapshot read for the sender's
	// model projection.
	byName := make(map[string]*enterworld.Character)
	if request.Kind == GroupLetterKindUnion {
		for _, candidate := range deps.CharactersForDivision(divisionID) {
			if candidate != nil {
				byName[strings.ToLower(candidate.Name)] = candidate
			}
		}
	}
	var senderSnapshot *enterworld.Character
	deps.Read(divisionID, func() {
		senderSnapshot = sender.Snapshot()
	})
	if senderSnapshot == nil {
		return refusedGroupLetterSend("sender snapshot unavailable")
	}
	body := request.Body
	if len(body) > LetterBodyMaxBytes {
		body = body[:LetterBodyMaxBytes]
	}
	letter := enterworld.LetterRecord{
		Sender:            sender.Name,
		SenderModelRefID:  deps.CharacterModelRef(senderSnapshot),
		PackedReceiveTime: PackLetterReceiveTime(now),
		ReadFlag:          0,
		Body:              body,
	}
	outcome := GroupLetterSendOutcome{}
	failed := []string{}
	for _, rcpt := range kept {
		receiverID := rcpt.charID
		receiverName := rcpt.requested
		if request.Kind == GroupLetterKindUnion {
			receiver := byName[strings.ToLower(rcpt.requested)]
			if receiver == nil {
				continue // unknown name: the native store join skips it
			}
			receiverID = receiver.ID
			receiverName = receiver.Name
		}
		switch letters.DeliverLetter(divisionID, sender.ID, receiverID, LetterMailboxMaxCount, letter) {
		case enterworld.LetterDeliveryDone:
			outcome.Deliveries = append(outcome.Deliveries, GroupLetterDelivery{
				RecipientName: receiverName,
				PushPayload:   EncodeLetterReceivedEvent3F9A(letter.Sender, letter.SenderModelRefID, letter.PackedReceiveTime),
			})
		case enterworld.LetterDeliveryMailboxFull:
			failed = append(failed, rcpt.requested)
		default:
			// Unavailable (gone or delete-pending): skipped without
			// a word, like an unknown name.
		}
	}
	outcome.AckPayload = EncodeGroupLetterSendAckB600(failed)
	return outcome
}
