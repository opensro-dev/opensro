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
	// accepts (QNO_WC_POTION_4's _01 [NEXT]).
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
reply row that turns the page (Rahid 5's pages _01.._08). A page
with a RefuseSymbol adds it as the second row; choosing it refuses the
quest through NpcQuestHooks.Refuse (KT_SMITH_2's _01 page).
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
	// OpenResuscitation answers the herbalist's withdrawal row (talk
	// mask 0x200) with no dialog stage: the native 0x3230 open, or the
	// owner's line for a player it refuses. Nil refuses the row.
	OpenResuscitation func(character *enterworld.Character, npcCodename string) ([]wire.Frame, error)
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

// The reason classes a refused 0x3773 choice is counted under (#2293).
// Classes stay coarse on purpose: the counted thing is why the choice
// could not run, not the formatted refusal line.
const (
	npcDialogRefusalMalformed      = "malformed choice"
	npcDialogRefusalNoConversation = "no active conversation"
	npcDialogRefusalNpcUnselected  = "dialog NPC no longer selected"
	npcDialogRefusalNpcOutOfScope  = "dialog NPC no longer live/in scope"
	npcDialogRefusalChoiceMismatch = "choice does not match the open conversation"
	npcDialogRefusalOwnerMissing   = "quest or service owner unavailable"
	npcDialogRefusalUnknownStage   = "unknown dialog stage"
)

/*
================
npcDialogRefusalCounts

Refused dialog choices counted per reason class; a class's first
refusal also logs at info, so a live server sees each reason once
without a debug build (#2293). Later refusals of a class only move
its count.
================
*/
type npcDialogRefusalCounts struct {
	mu      sync.Mutex
	byClass map[string]int64
}

/*
================
note

Counts one refusal of the class and reports whether it is the first.
================
*/
func (c *npcDialogRefusalCounts) note(class string) (count int64, first bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byClass == nil {
		c.byClass = make(map[string]int64)
	}
	c.byClass[class]++
	return c.byClass[class], c.byClass[class] == 1
}

/*
================
count

The refusals counted for the class so far.
================
*/
func (c *npcDialogRefusalCounts) count(class string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byClass[class]
}

/*
================
Runtime.noteNpcDialogRefusal

Counts the refusal; the class's first one logs at info with the
refusal line itself, so the reason is visible once per class.
================
*/
func (rt *Runtime) noteNpcDialogRefusal(class, characterName, refusal string) {
	if _, first := rt.npcDialogRefusals.note(class); first {
		log.Infof("npcdialog: first %s refusal (for %s): %s; later refusals of this class are counted, not logged", class, characterName, refusal)
	}
}

/*
================
Runtime.refuseNpcDialogChoice

The refused-choice answer for a choice that had a conversation: the
conversation ends and the NPC's base line answers with its Exit row,
the shape every refused quest branch in this file already uses, so the
client's talk pane is never left waiting on a choice the server will
not run. The refusal is counted under its reason class.
================
*/
func (rt *Runtime) refuseNpcDialogChoice(divisionID string, character *enterworld.Character, conversation npcDialogSession, class, refusal string) ([]wire.Frame, string) {
	rt.NpcDialogs.Clear(divisionID, character.Name)
	rt.noteNpcDialogRefusal(class, character.Name, refusal)
	return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}, refusal
}

/*
================
Runtime.countNpcDialogRefusal

A refusal that keeps its conversation: the stored conversation is
still the live one (the choice byte matched no row on screen, which
the client cannot send from the rows it shows), so the pane is not
stuck and no line replaces it; the refusal is only counted.
================
*/
func (rt *Runtime) countNpcDialogRefusal(character *enterworld.Character, class, refusal string) ([]wire.Frame, string) {
	rt.noteNpcDialogRefusal(class, character.Name, refusal)
	return nil, refusal
}

/*
================
Runtime.refuseNpcDialogWithoutConversation

A choice with no stored conversation behind it: when the still-selected
NPC is live, its base line answers with the Exit row, so a talk pane the
server has forgotten still gets its closing line. With nothing selected
or live there is no line to answer with; the refusal is only counted.
================
*/
func (rt *Runtime) refuseNpcDialogWithoutConversation(divisionID string, character *enterworld.Character, class, refusal string) ([]wire.Frame, string) {
	rt.noteNpcDialogRefusal(class, character.Name, refusal)
	if selected, bound := rt.Selected.Get(divisionID, character.Name); bound {
		if npc, live := rt.npcForCurrentViewer(divisionID, character, selected); live && npc.BaseSpeechSymbol != "" {
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(npc.BaseSpeechSymbol)}}, refusal
		}
	}
	return nil, refusal
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
			// A refused choice still answers (the closing dialog line);
			// the reason string is only the log line (#2293).
			log.Debugf("npcdialog: response refused for %s: %s", character.Name, refusal)
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
Every refusal is counted per reason class. A stale or unrunnable choice
also answers the closing dialog line (refuseNpcDialogChoice), since
silence would leave the client's talk pane waiting (#2293). A choice byte
that matches no row keeps the live conversation and is only counted.
================
*/
func (rt *Runtime) HandleNpcDialogResponse(divisionID string, character *enterworld.Character, payload []byte) ([]wire.Frame, string) {
	conversation, hasConversation := rt.NpcDialogs.Get(divisionID, character.Name)
	choice, err := wire.DecodeNpcDialogChoice(payload)
	if err != nil {
		if hasConversation {
			return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalMalformed, err.Error())
		}
		return rt.refuseNpcDialogWithoutConversation(divisionID, character, npcDialogRefusalMalformed, err.Error())
	}
	if !hasConversation {
		// Informational Confirm terminates the interaction: later server
		// quest talk does the same; v1.150 client 761820 closes on B4B3 [1].
		// Re-sending the base prompt creates an endless Confirm loop. The
		// Exit row of a closing answer (EncodeNpcDialogAnswer) ends it too.
		if choice == 1 || choice == npcDialogFirstRow {
			if selected, bound := rt.Selected.Get(divisionID, character.Name); bound {
				if _, live := rt.npcForCurrentViewer(divisionID, character, selected); live {
					outcome := rt.HandleTargetRelease(divisionID, character, wire.NewWriter(4).U32(selected).Payload())
					return outcome.Frames, outcome.Refusal
				}
			}
		}
		return rt.refuseNpcDialogWithoutConversation(divisionID, character, npcDialogRefusalNoConversation, "no active NPC dialog")
	}
	selected, ok := rt.Selected.Get(divisionID, character.Name)
	if !ok || selected != conversation.NpcGID {
		return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalNpcUnselected, "dialog NPC is no longer selected")
	}
	if _, live := rt.npcForCurrentViewer(divisionID, character, selected); !live {
		return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalNpcOutOfScope, "dialog NPC is no longer live/in scope")
	}

	switch conversation.Stage {
	case npcDialogOptions:
		if choice < 5 || int(choice-5) >= len(conversation.Options) {
			return rt.countNpcDialogRefusal(character, npcDialogRefusalChoiceMismatch, fmt.Sprintf("choice %d is outside %d option row(s)", choice, len(conversation.Options)))
		}
		conversation.Pending = conversation.Options[int(choice-5)]
		if conversation.Pending.Immediate {
			if rt.NpcQuests.Finish == nil {
				return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalOwnerMissing, "NPC service owner is unavailable")
			}
			frames, serviceError := rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
			rt.NpcDialogs.Clear(divisionID, character.Name)
			if serviceError != nil {
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}, ""
			}
			return frames, ""
		}
		if conversation.Pending.SideTalk {
			// The native quest dialog speaks the pending line and clears its bit on the next
			// step; closing the line is that step, so it is recorded here.
			rt.NpcDialogs.Clear(divisionID, character.Name)
			if rt.NpcQuests.Finish == nil {
				return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalOwnerMissing, "NPC quest owner is unavailable")
			}
			frames, heardError := rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
			if heardError != nil {
				log.Debugf("npcdialog: side talk %s refused: %v", conversation.Pending.Codename, heardError)
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}, ""
			}
			return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.Pending.PromptSymbol)}), ""
		}
		if conversation.Pending.Informational {
			rt.NpcDialogs.Clear(divisionID, character.Name)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.Pending.PromptSymbol)}}, ""
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
			return rt.countNpcDialogRefusal(character, npcDialogRefusalChoiceMismatch, fmt.Sprintf("page choice %d is not the page's reply row", choice))
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
			return rt.countNpcDialogRefusal(character, npcDialogRefusalChoiceMismatch, fmt.Sprintf("branch choice %d is outside %d reply row(s)", choice, len(conversation.Pending.Branches)+1))
		}
		rt.NpcDialogs.Clear(divisionID, character.Name)
		if row == len(conversation.Pending.Branches) {
			symbol := conversation.Pending.DenyResponseSymbol
			if symbol == "" {
				symbol = conversation.DefaultSymbol
			}
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(symbol)}}, ""
		}
		if rt.NpcQuests.Accept == nil {
			return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalOwnerMissing, "quest acceptance owner is unavailable")
		}
		branch := conversation.Pending.Branches[row]
		frames, acceptError := rt.NpcQuests.Accept(character, branch.Codename)
		if acceptError != nil {
			log.Debugf("npcdialog: quest branch %s refused: %v", branch.Codename, acceptError)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}, ""
		}
		symbol := branch.AcceptResponseSymbol
		if symbol == "" {
			symbol = conversation.DefaultSymbol
		}
		return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(symbol)}), ""
	case npcDialogAcceptRow:
		// The offer's only row is its acceptance: answer it as confirm's yes.
		if choice != npcDialogFirstRow {
			return rt.countNpcDialogRefusal(character, npcDialogRefusalChoiceMismatch, fmt.Sprintf("accept row choice %d is not the offer's row", choice))
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
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(symbol)}}, ""
		}
		if choice != npcDialogConfirmYes {
			return rt.countNpcDialogRefusal(character, npcDialogRefusalChoiceMismatch, fmt.Sprintf("confirm choice %d is neither yes(2) nor no(3)", choice))
		}
		var frames []wire.Frame
		if conversation.Pending.Complete {
			if rt.NpcQuests.Finish == nil {
				return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalOwnerMissing, "quest completion owner is unavailable")
			}
			frames, err = rt.NpcQuests.Finish(character, conversation.Pending.Codename, conversation.NpcCode)
		} else {
			if rt.NpcQuests.Accept == nil {
				return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalOwnerMissing, "quest acceptance owner is unavailable")
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
				return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(localized.DialogueSymbol())}}, ""
			}
			// Eligibility may change after the offer (level, inventory or quest
			// state). End this confirmation and answer with the authored base
			// prompt; never keep a stale Yes action or silently strand a request.
			log.Debugf("npcdialog: quest %s refused: %v", conversation.Pending.Codename, err)
			rt.NpcDialogs.Clear(divisionID, character.Name)
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}, ""
		}
		rt.NpcDialogs.Clear(divisionID, character.Name)
		symbol := conversation.Pending.AcceptResponseSymbol
		if symbol == "" {
			symbol = conversation.DefaultSymbol
		}
		return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(symbol)}), ""
	default:
		return rt.refuseNpcDialogChoice(divisionID, character, conversation, npcDialogRefusalUnknownStage, "unknown NPC dialog stage")
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
			return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(symbol)}}, ""
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
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}
	}
	frames, err := rt.NpcQuests.Refuse(character, conversation.Pending.Codename)
	if err != nil {
		log.Debugf("npcdialog: quest refusal %s refused: %v", conversation.Pending.Codename, err)
		return []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(conversation.DefaultSymbol)}}
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(page.RefuseResponseSymbol)})
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
