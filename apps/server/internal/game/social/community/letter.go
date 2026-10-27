package community

import (
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// The letter (memo) lane: 0x7261 send, 0x73F2 read-fetch, 0x70CC delete
// over the authority store's memos table (enterworld.LetterStore). All
// wire layouts are pinned from the client folds cited in wire.go; every
// refusal is SILENT (the war-horn/COS convention) because the only error
// channels the client parses - 0xB261/0xB3F2 result 2 - carry error
// CODES whose values are unpinned vs retail.
//
// Index contract: the u8 wire index is the mailbox slice position. The
// seed (0xB3CD), the live push (0x3F9A case 8, a tail append on both
// sides), the read answer and the delete ack all keep the server list
// and the client's ced160+0x24c list aligned move for move, so the index
// never needs a stable id column.

// PackLetterReceiveTime composes the packed receive-time dword the
// client's sub_60c9b0 decode reads (record +0x3c): bits 0..5 the
// two-digit year, 6..9 the month, 10..14 the day, 15..19 the hour,
// 20..25 the minute. The year field is 6 bits, so years past 2063 alias
// - exactly what the native field does; the client renders it %0.2d.
func PackLetterReceiveTime(t time.Time) uint32 {
	year := uint32(t.Year()%100) & 0x3f
	month := uint32(t.Month()) & 0xf
	day := uint32(t.Day()) & 0x1f
	hour := uint32(t.Hour()) & 0x1f
	minute := uint32(t.Minute()) & 0x3f
	return year | month<<6 | day<<10 | hour<<15 | minute<<20
}

// letterJobAliasLead is the job-alias lead character the client's own
// composer blocks before the wire (client NetClient_Send7261
// against L"*"): a frame carrying it bypassed the client gate and
// is refused.
const letterJobAliasLead = "*"

// LetterSendOutcome is one handled 0x7261 request. AckPayload is the
// 0xB261 result-1 body for the SENDER; RecipientPushPayload is the
// 0x3F9A case-8 body for the RECIPIENT's live session when one exists
// (the hub adapter resolves it - never inside the store door). A refusal
// leaves both nil and the wire silent.
type LetterSendOutcome struct {
	AckPayload           []byte
	RecipientName        string
	RecipientPushPayload []byte
	Refusal              string
}

func refusedLetterSend(reason string) LetterSendOutcome {
	return LetterSendOutcome{Refusal: reason}
}

// HandleLetterSend applies one decoded 0x7261 request: resolve the
// receiver character through the store (never a session), append the
// letter to THEIR mailbox through the letter door, and ack the sender
// with the pinned 0xB261 success byte. Refusals mirror the client's own
// gates (empty fields, the 0xc/0x100 edit caps, the job-alias lead) plus
// the server-side truths the client cannot know (unknown receiver,
// delete-pending receiver, the 0x14 mailbox cap) - all silent.
func HandleLetterSend(deps Dependencies, divisionID string, sender *enterworld.Character, payload []byte, now time.Time) LetterSendOutcome {
	if sender == nil {
		return refusedLetterSend("characterNotFound")
	}
	letters := deps.LetterAuthority()
	if letters == nil {
		return refusedLetterSend("no letter store wired")
	}
	request, err := DecodeLetterSendRequest(payload)
	if err != nil {
		return refusedLetterSend(err.Error())
	}
	if request.Receiver == "" {
		return refusedLetterSend("receiver is empty")
	}
	if request.Body == "" {
		return refusedLetterSend("body is empty")
	}
	if strings.HasPrefix(request.Receiver, letterJobAliasLead) {
		// The client's own composer never puts a job-alias receiver on
		// the wire (UIIT_MSG_MEMO_NOTUSE_JOB); reaching here is a bypass.
		return refusedLetterSend("job-alias receiver bypassed the client gate")
	}
	if len(request.Receiver) > LetterReceiverMaxBytes {
		return refusedLetterSend(fmt.Sprintf("receiver %d bytes exceeds the client edit cap %d", len(request.Receiver), LetterReceiverMaxBytes))
	}
	if len(request.Body) > LetterBodyMaxBytes {
		return refusedLetterSend(fmt.Sprintf("body %d bytes exceeds the client edit cap %d", len(request.Body), LetterBodyMaxBytes))
	}

	var receiver *enterworld.Character
	for _, candidate := range deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, request.Receiver) {
			receiver = candidate
			break
		}
	}
	if receiver == nil {
		return refusedLetterSend(fmt.Sprintf("receiver %q not found in division %s", request.Receiver, divisionID))
	}
	var senderSnapshot, receiverSnapshot *enterworld.Character
	deps.Read(divisionID, func() {
		senderSnapshot = sender.Snapshot()
		receiverSnapshot = receiver.Snapshot()
	})
	if senderSnapshot == nil || senderSnapshot.DeletePending {
		return refusedLetterSend("deletePending")
	}
	if receiverSnapshot == nil || receiverSnapshot.DeletePending {
		return refusedLetterSend(fmt.Sprintf("receiver %q is delete-pending", receiver.Name))
	}

	letter := enterworld.LetterRecord{
		Sender:            sender.Name,
		SenderModelRefID:  deps.CharacterModelRef(senderSnapshot),
		PackedReceiveTime: PackLetterReceiveTime(now),
		ReadFlag:          0,
		Body:              request.Body,
	}
	if !letters.DeliverLetter(
		divisionID,
		sender.ID,
		receiver.ID,
		LetterMailboxMaxCount,
		letter,
	) {
		return refusedLetterSend(fmt.Sprintf(
			"delivery to %q refused inside the authority door (character unavailable or mailbox full)",
			receiver.Name,
		))
	}
	outcome := LetterSendOutcome{}
	outcome.AckPayload = EncodeLetterSendAckB261()
	outcome.RecipientName = receiver.Name
	outcome.RecipientPushPayload = EncodeLetterReceivedEvent3F9A(letter.Sender, letter.SenderModelRefID, letter.PackedReceiveTime)
	return outcome
}

// LetterAnswerOutcome is one handled 0x73F2 or 0x70CC request: the
// pinned result-1 answer body, or a silent refusal.
type LetterAnswerOutcome struct {
	AnswerPayload []byte
	Refusal       string
}

func refusedLetterAnswer(reason string) LetterAnswerOutcome {
	return LetterAnswerOutcome{Refusal: reason}
}

// HandleLetterReadFetch applies one decoded 0x73F2 request: answer the
// 0xB3F2 result-1 body for the index-th letter and flip the persisted
// read flag when it was unread - the same 0-check the client's own
// CharacterDependentData_StoreLetterBodyAndMarkRead store performs on
// its side of the flip.
func HandleLetterReadFetch(deps Dependencies, divisionID string, character *enterworld.Character, payload []byte) LetterAnswerOutcome {
	if character == nil {
		return refusedLetterAnswer("characterNotFound")
	}
	letters := deps.LetterAuthority()
	if letters == nil {
		return refusedLetterAnswer("no letter store wired")
	}
	index, err := DecodeLetterIndexRequest(payload)
	if err != nil {
		return refusedLetterAnswer(err.Error())
	}
	outcome := LetterAnswerOutcome{}
	updated := letters.UpdateMailbox(divisionID, character.ID, "letter-read", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		if int(index) >= len(mailbox) {
			outcome.Refusal = fmt.Sprintf("index %d out of range (%d letter(s))", index, len(mailbox))
			return mailbox, false
		}
		changed := mailbox[index].ReadFlag == 0
		if mailbox[index].ReadFlag == 0 {
			mailbox[index].ReadFlag = 1
		}
		outcome.AnswerPayload = EncodeLetterReadBodyB3F2(index, mailbox[index].Body)
		return mailbox, changed
	})
	if !updated && outcome.AnswerPayload == nil && outcome.Refusal == "" {
		outcome.Refusal = "character unavailable inside the mailbox authority door"
	}
	return outcome
}

// HandleLetterDelete applies one decoded 0x70CC request: remove the
// index-th letter from the persisted mailbox and answer the pinned
// 0xB0CC result-1 ack - the client erases at the same index, so the two
// lists stay aligned.
func HandleLetterDelete(deps Dependencies, divisionID string, character *enterworld.Character, payload []byte) LetterAnswerOutcome {
	if character == nil {
		return refusedLetterAnswer("characterNotFound")
	}
	letters := deps.LetterAuthority()
	if letters == nil {
		return refusedLetterAnswer("no letter store wired")
	}
	index, err := DecodeLetterIndexRequest(payload)
	if err != nil {
		return refusedLetterAnswer(err.Error())
	}
	outcome := LetterAnswerOutcome{}
	updated := letters.UpdateMailbox(divisionID, character.ID, "letter-delete", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		if int(index) >= len(mailbox) {
			outcome.Refusal = fmt.Sprintf("index %d out of range (%d letter(s))", index, len(mailbox))
			return mailbox, false
		}
		next := make([]enterworld.LetterRecord, 0, len(mailbox)-1)
		next = append(next, mailbox[:index]...)
		next = append(next, mailbox[index+1:]...)
		outcome.AnswerPayload = EncodeLetterDeleteAckB0CC(index)
		return next, true
	})
	if !updated && outcome.AnswerPayload == nil && outcome.Refusal == "" {
		outcome.Refusal = "character unavailable inside the mailbox authority door"
	}
	return outcome
}

// MailboxListEntries projects a persisted mailbox onto the 0xB3CD
// result-1 rows (the body stays server-side until a 0x73F2 fetch).
func MailboxListEntries(mailbox []enterworld.LetterRecord) []LetterListEntry {
	out := make([]LetterListEntry, 0, len(mailbox))
	for _, letter := range mailbox {
		out = append(out, LetterListEntry{
			Sender:            letter.Sender,
			SenderModelRefID:  letter.SenderModelRefID,
			PackedReceiveTime: letter.PackedReceiveTime,
			ReadFlag:          letter.ReadFlag,
		})
	}
	return out
}

// RegisterLetter wires the letter lane onto the hub: 0x7261 send, 0x73F2
// read-fetch, 0x70CC delete (replacing their silent-refuse stubs). Wired
// from wiring.go with the SAME deps pointer every other lane retains.
// A refusal stays SILENT (the only pinned error
// arms carry unpinned error-code values) and logs its reason; success
// answers with the pinned 0xB261/0xB3F2/0xB0CC result-1 bodies. The
// send handler additionally pushes the pinned 0x3F9A case-8 event to the
// RECIPIENT's live session when presence resolves one - AFTER the mailbox
// door returned (never inside the store lock); an offline recipient is
// served by their next enter-world's 0xB3CD seed.
func RegisterLetter(hub *transport.Hub, deps Dependencies, presence Presence) {
	hub.Handle(OpLetterSendRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		sender, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleLetterSend(deps, divisionID, sender, payload, time.Now())
		if outcome.Refusal != "" {
			log.Debugf("community: 0x7261 (letter-send) refused for %s: %s", sender.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpLetterSendAckAnswer, outcome.AckPayload)
		if peer, online := presenceSession(presence, divisionID, outcome.RecipientName); online {
			_ = peer.Send(OpFriendLetterEventPush, outcome.RecipientPushPayload)
		}
		log.Debugf("community: letter from %s delivered to %s's mailbox", sender.Name, outcome.RecipientName)
	})
	hub.Handle(OpLetterReadFetchRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleLetterReadFetch(deps, divisionID, character, payload)
		if outcome.Refusal != "" {
			log.Debugf("community: 0x73F2 (letter-read-fetch) refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpLetterReadBodyAnswer, outcome.AnswerPayload)
	})
	hub.Handle(OpLetterDeleteRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleLetterDelete(deps, divisionID, character, payload)
		if outcome.Refusal != "" {
			log.Debugf("community: 0x70CC (letter-delete) refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		_ = s.Send(OpLetterDeleteAckAnswer, outcome.AnswerPayload)
	})
}
