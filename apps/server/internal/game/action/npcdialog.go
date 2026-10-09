/*
===========================================================================

npcdialog.go - bind native dialog choices to one live NPC conversation

Selection and dialog identity are checked before a quest or service runs.
The quest owner changes persistent state; this module owns transient choices
and the response that ends each request, including immediate service windows.

===========================================================================
*/
package action

import (
	"errors"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
NpcQuestOption

Symbols are client-localized. Immediate services run through Finish when the
row is selected; ordinary quests first ask for acceptance or completion.
Pages, when present, are shown one by one before that question. A SideTalk
row speaks its line and records it as heard through Finish.
================
*/
type NpcQuestOption struct {
	Codename             string
	TitleSymbol          string
	PromptSymbol         string
	AcceptResponseSymbol string
	DenyResponseSymbol   string
	Pages                []NpcDialogPage
	Branches             []NpcDialogBranch
	Informational        bool
	SideTalk             bool
	Complete             bool
	Immediate            bool
	// AcceptRowSymbol replaces an offer's yes/no with this one row, which
	// accepts (QNO_WC_POTION_4's _01 [NEXT], 897680).
	AcceptRowSymbol string
}

/*
================
NpcDialogBranch

One reply of a branching offer: the token Accept receives and the line the
NPC answers with (Rahid 2's _02 feathers / _06 waiting).
================
*/
type NpcDialogBranch struct {
	Codename             string
	ReplySymbol          string
	AcceptResponseSymbol string
}

// npcDialogRefuseRow is the refusal row a branching offer ends with.
const npcDialogRefuseRow = "SN_TALK_COMMON_DENY"

/*
================
NpcDialogPage
One page of an NPC's story before a quest offer: a prompt and the single
reply row that turns the page (Rahid 5's 8A03F0 pages _01.._08). A page
with a RefuseSymbol adds it as the second row; choosing it refuses the
quest through NpcQuestHooks.Refuse (KT_SMITH_2's _01 page, 8A77A0).
================
*/
type NpcDialogPage struct {
	PromptSymbol         string
	ReplySymbol          string
	RefuseSymbol         string
	RefuseResponseSymbol string
}

/*
================
NpcQuestHooks

The quest runtime never learns selected GIDs or wire row numbers.
================
*/
type NpcQuestHooks struct {
	Options func(divisionID string, character *enterworld.Character, npcCodename string) []NpcQuestOption
	Prepare func(character *enterworld.Character, codename, npcCodename string) (string, error)
	Accept  func(character *enterworld.Character, codename string) ([]wire.Frame, error)
	Finish  func(character *enterworld.Character, codename, npcCodename string) ([]wire.Frame, error)
	Refuse  func(character *enterworld.Character, codename string) ([]wire.Frame, error)
}

/*
================
npcDialogStage
================
*/
type npcDialogStage uint8

const (
	npcDialogOptions npcDialogStage = iota + 1
	npcDialogConfirm
	npcDialogPages
	npcDialogBranches
	npcDialogAcceptRow
)

// npcDialogFirstRow is the client's choice byte for a kind-4 dialog's first row.
const npcDialogFirstRow = 5

// npcDialogConfirmYes is the client's choice byte for a confirm dialog's yes.
const npcDialogConfirmYes = 2

/*
================
npcDialogSession
================
*/
type npcDialogSession struct {
	NpcGID        uint32
	NpcCode       string
	DefaultSymbol string
	Stage         npcDialogStage
	Options       []NpcQuestOption
	Pending       NpcQuestOption
	// Page is the story page on screen while Stage is npcDialogPages.
	Page int
}

/*
================
NpcDialogStore

Selection replacement, target release and mission exit clear the conversation
so a delayed one-byte choice cannot apply to a different NPC.
================
*/
type NpcDialogStore struct {
	mu          sync.Mutex
	byCharacter map[string]npcDialogSession
}

/*
================
NewNpcDialogStore
================
*/
func NewNpcDialogStore() *NpcDialogStore {
	return &NpcDialogStore{byCharacter: make(map[string]npcDialogSession)}
}

/*
================
Put
================
*/
func (s *NpcDialogStore) Put(divisionID, characterName string, session npcDialogSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session.Options = append([]NpcQuestOption(nil), session.Options...)
	s.byCharacter[selectionKey(divisionID, characterName)] = session
}

/*
================
Get
================
*/
func (s *NpcDialogStore) Get(divisionID, characterName string) (npcDialogSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byCharacter[selectionKey(divisionID, characterName)]
	session.Options = append([]NpcQuestOption(nil), session.Options...)
	return session, ok
}

/*
================
Clear
================
*/
func (s *NpcDialogStore) Clear(divisionID, characterName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byCharacter, selectionKey(divisionID, characterName))
}

/*
================
registerNpcDialogResponse
================
*/
func (rt *Runtime) registerNpcDialogResponse(hub *transport.Hub) {
	hub.Handle(wire.OpNpcDialog, func(session *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			return
		}
		frames, refusal := rt.HandleNpcDialogResponse(divisionID, character, payload)
		if refusal != "" {
			log.Debugf("npcdialog: response refused for %s: %s", character.Name, refusal)
			return
		}
		sendFrames(session, frames)
		if public := wire.ProgressionBroadcastFrames(frames); len(public) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(divisionID, character.Name, public)
		}
	})
}

/*
================
HandleNpcDialogResponse

Rebind every one-byte choice to the still-live selected NPC before calling
a quest mutation or service. Clear immediate choices before returning frames.
================
*/
func (rt *Runtime) HandleNpcDialogResponse(divisionID string, character *enterworld.Character, payload []byte) ([]wire.Frame, string) {
	choice, err := wire.DecodeNpcDialogChoice(payload)
	if err != nil {
		return nil, err.Error()
	}
	conversation, ok := rt.NpcDialogs.Get(divisionID, character.Name)
	if !ok {
		// Informational Confirm terminates the interaction: later server
		// 516670 -> 5107C0, v1.150 client 761820 closes on B4B3 [1].
		// Re-sending the base prompt creates an endless Confirm loop.
		if choice == 1 {
			if selected, bound := rt.Selected.Get(divisionID, character.Name); bound {
				if _, live := rt.npcForCurrentViewer(divisionID, character, selected); live {
					outcome := rt.HandleTargetRelease(divisionID, character, wire.NewWriter(4).U32(selected).Payload())
					return outcome.Frames, outcome.Refusal
				}
			}
		}
		return nil, "no active NPC dialog"
	}
	selected, ok := rt.Selected.Get(divisionID, character.Name)
	if !ok || selected != conversation.NpcGID {
		rt.NpcDialogs.Clear(divisionID, character.Name)
		return nil, "dialog NPC is no longer selected"
	}
	if _, live := rt.npcForCurrentViewer(divisionID, character, selected); !live {
		rt.NpcDialogs.Clear(divisionID, character.Name)
		return nil, "dialog NPC is no longer live/in scope"
	}

	switch conversation.Stage {
	case npcDialogOptions:
		if choice < 5 || int(choice-5) >= len(conversation.Options) {
			return nil, fmt.Sprintf("choice %d is outside %d option row(s)", choice, len(conversation.Options))
		}
		conversation.Pending = conversation.Options[int(choice-5)]
		if conversation.Pending.Immediate {
			if rt.NpcQuests.Finish == nil {
				return nil, "NPC service owner is unavailable"
			}
			frames, serviceError := rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
			rt.NpcDialogs.Clear(divisionID, character.Name)
			if serviceError != nil {
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}, ""
			}
			return frames, ""
		}
		if conversation.Pending.SideTalk {
			// 89FDA0 speaks the pending line and clears its bit on the next
			// step; closing the line is that step, so it is recorded here.
			rt.NpcDialogs.Clear(divisionID, character.Name)
			if rt.NpcQuests.Finish == nil {
				return nil, "NPC quest owner is unavailable"
			}
			frames, heardError := rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
			if heardError != nil {
				log.Debugf("npcdialog: side talk %s refused: %v", conversation.Pending.Codename, heardError)
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}, ""
			}
			return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.Pending.PromptSymbol)}), ""
		}
		if conversation.Pending.Informational {
			rt.NpcDialogs.Clear(divisionID, character.Name)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.Pending.PromptSymbol)}}, ""
		}
		conversation.Options = nil
		if len(conversation.Pending.Pages) > 0 {
			conversation.Stage, conversation.Page = npcDialogPages, 0
			rt.NpcDialogs.Put(divisionID, character.Name, conversation)
			return []wire.Frame{npcDialogPageFrame(conversation.Pending.Pages[0])}, ""
		}
		return rt.openNpcDialogConfirm(divisionID, character, conversation)
	case npcDialogPages:
		page := conversation.Pending.Pages[conversation.Page]
		if page.RefuseSymbol != "" && choice == npcDialogFirstRow+1 {
			return rt.refuseNpcQuest(divisionID, character, conversation, page), ""
		}
		if choice != npcDialogFirstRow {
			return nil, fmt.Sprintf("page choice %d is not the page's reply row", choice)
		}
		conversation.Page++
		if conversation.Page < len(conversation.Pending.Pages) {
			rt.NpcDialogs.Put(divisionID, character.Name, conversation)
			return []wire.Frame{npcDialogPageFrame(conversation.Pending.Pages[conversation.Page])}, ""
		}
		return rt.openNpcDialogConfirm(divisionID, character, conversation)
	case npcDialogBranches:
		row := int(choice) - npcDialogFirstRow
		if row < 0 || row > len(conversation.Pending.Branches) {
			return nil, fmt.Sprintf("branch choice %d is outside %d reply row(s)", choice, len(conversation.Pending.Branches)+1)
		}
		rt.NpcDialogs.Clear(divisionID, character.Name)
		if row == len(conversation.Pending.Branches) {
			symbol := conversation.Pending.DenyResponseSymbol
			if symbol == "" {
				symbol = conversation.DefaultSymbol
			}
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}}, ""
		}
		if rt.NpcQuests.Accept == nil {
			return nil, "quest acceptance owner is unavailable"
		}
		branch := conversation.Pending.Branches[row]
		frames, acceptError := rt.NpcQuests.Accept(character, branch.Codename)
		if acceptError != nil {
			log.Debugf("npcdialog: quest branch %s refused: %v", branch.Codename, acceptError)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}, ""
		}
		symbol := branch.AcceptResponseSymbol
		if symbol == "" {
			symbol = conversation.DefaultSymbol
		}
		return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}), ""
	case npcDialogAcceptRow:
		// The offer's only row is its acceptance: answer it as confirm's yes.
		if choice != npcDialogFirstRow {
			return nil, fmt.Sprintf("accept row choice %d is not the offer's row", choice)
		}
		choice = npcDialogConfirmYes
		fallthrough
	case npcDialogConfirm:
		if choice == 3 {
			rt.NpcDialogs.Clear(divisionID, character.Name)
			symbol := conversation.Pending.DenyResponseSymbol
			if symbol == "" {
				symbol = conversation.DefaultSymbol
			}
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}}, ""
		}
		if choice != npcDialogConfirmYes {
			return nil, fmt.Sprintf("confirm choice %d is neither yes(2) nor no(3)", choice)
		}
		var frames []wire.Frame
		if conversation.Pending.Complete {
			if rt.NpcQuests.Finish == nil {
				return nil, "quest completion owner is unavailable"
			}
			frames, err = rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
		} else {
			if rt.NpcQuests.Accept == nil {
				return nil, "quest acceptance owner is unavailable"
			}
			frames, err = rt.NpcQuests.Accept(character, conversation.Pending.Codename)
		}
		if err != nil {
			var localized interface {
				error
				DialogueSymbol() string
			}
			if errors.As(err, &localized) && localized.DialogueSymbol() != "" {
				rt.NpcDialogs.Clear(divisionID, character.Name)
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(localized.DialogueSymbol())}}, ""
			}
			// Eligibility may change after the offer (level, inventory or quest
			// state). End this confirmation and answer with the authored base
			// prompt; never keep a stale Yes action or silently strand a request.
			log.Debugf("npcdialog: quest %s refused: %v", conversation.Pending.Codename, err)
			rt.NpcDialogs.Clear(divisionID, character.Name)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}, ""
		}
		rt.NpcDialogs.Clear(divisionID, character.Name)
		symbol := conversation.Pending.AcceptResponseSymbol
		if symbol == "" {
			symbol = conversation.DefaultSymbol
		}
		return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}), ""
	default:
		return nil, "unknown NPC dialog stage"
	}
}

/*
================
openNpcDialogConfirm

Prepares the pending quest row and asks its acceptance or completion
question. The caller has already left the options or pages stage.
================
*/
func (rt *Runtime) openNpcDialogConfirm(divisionID string, character *enterworld.Character, conversation npcDialogSession) ([]wire.Frame, string) {
	conversation.Stage = npcDialogConfirm
	if rt.NpcQuests.Prepare != nil {
		prepared, err := rt.NpcQuests.Prepare(character, conversation.Pending.Codename, conversation.NpcCode)
		if err != nil {
			rt.NpcDialogs.Clear(divisionID, character.Name)
			symbol := conversation.DefaultSymbol
			var localized interface {
				error
				DialogueSymbol() string
			}
			if errors.As(err, &localized) && localized.DialogueSymbol() != "" {
				symbol = localized.DialogueSymbol()
			}
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}}, ""
		}
		conversation.Pending.Codename = prepared
	}
	if len(conversation.Pending.Branches) > 0 && !conversation.Pending.Complete {
		conversation.Stage = npcDialogBranches
		rows := make([]string, 0, len(conversation.Pending.Branches)+1)
		for _, branch := range conversation.Pending.Branches {
			rows = append(rows, branch.ReplySymbol)
		}
		rows = append(rows, npcDialogRefuseRow)
		rt.NpcDialogs.Put(divisionID, character.Name, conversation)
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogOptions(conversation.Pending.PromptSymbol, rows)}}, ""
	}
	if conversation.Pending.AcceptRowSymbol != "" && !conversation.Pending.Complete {
		conversation.Stage = npcDialogAcceptRow
		rt.NpcDialogs.Put(divisionID, character.Name, conversation)
		rows := []string{conversation.Pending.AcceptRowSymbol}
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogOptions(conversation.Pending.PromptSymbol, rows)}}, ""
	}
	rt.NpcDialogs.Put(divisionID, character.Name, conversation)
	return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogConfirm(conversation.Pending.PromptSymbol)}}, ""
}

/*
================
refuseNpcQuest

A page's refusal row ends the conversation with the page's answer. When
the quest owner turns the refusal down (the offer no longer stands), the
NPC answers its base prompt instead and nothing is ended.
================
*/
func (rt *Runtime) refuseNpcQuest(divisionID string, character *enterworld.Character, conversation npcDialogSession, page NpcDialogPage) []wire.Frame {
	rt.NpcDialogs.Clear(divisionID, character.Name)
	if rt.NpcQuests.Refuse == nil {
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}
	}
	frames, err := rt.NpcQuests.Refuse(character, conversation.Pending.Codename)
	if err != nil {
		log.Debugf("npcdialog: quest refusal %s refused: %v", conversation.Pending.Codename, err)
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(conversation.DefaultSymbol)}}
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(page.RefuseResponseSymbol)})
}

/*
================
npcDialogPageFrame
================
*/
func npcDialogPageFrame(page NpcDialogPage) wire.Frame {
	rows := []string{page.ReplySymbol}
	if page.RefuseSymbol != "" {
		rows = append(rows, page.RefuseSymbol)
	}
	return wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogOptions(page.PromptSymbol, rows)}
}
