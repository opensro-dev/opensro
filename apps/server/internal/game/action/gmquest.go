/*
===========================================================================

gmquest.go - the GM's quest tools: /INITQ, /REMOVEQ, /COMPQ and /RESETQ

Subcommands 0x1B, 0x1E, 0x1D and 0x1C of the GM command (client 509CF0,
client 50CA69..50CBE6). The quest runtime owns the journal and its history
(quest/gm.go); this file finds the GM, hands the runtime the character and
delivers the frames it returns. The gmcommand dispatcher has already decoded
the codename and has answered nothing yet.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
GMQuestResult

The frames one GM quest tool produced: the private journal, inventory and
experience frames, and the level-up presentation peers may see.
================
*/
type GMQuestResult struct {
	Frames    []wire.Frame
	Broadcast []wire.Frame
}

/*
================
GMQuestHooks

The quest runtime never learns the division lock or the session delivery.
Complete returns the frames of the stages it committed even with an error.
================
*/
type GMQuestHooks struct {
	Init     func(character *enterworld.Character, codename string) (GMQuestResult, error)
	Remove   func(character *enterworld.Character, codename string) (GMQuestResult, error)
	Complete func(character *enterworld.Character, codename string) (GMQuestResult, error)
	Reset    func(character *enterworld.Character) (GMQuestResult, error)
}

/*
================
InitQuestGM
================
*/
func (rt *Runtime) InitQuestGM(division, name, codename string) bool {
	return rt.gmQuest(division, name, "INITQ", rt.GMQuests.Init != nil, func(c *enterworld.Character) (GMQuestResult, error) {
		return rt.GMQuests.Init(c, codename)
	})
}

/*
================
RemoveQuestGM
================
*/
func (rt *Runtime) RemoveQuestGM(division, name, codename string) bool {
	return rt.gmQuest(division, name, "REMOVEQ", rt.GMQuests.Remove != nil, func(c *enterworld.Character) (GMQuestResult, error) {
		return rt.GMQuests.Remove(c, codename)
	})
}

/*
================
CompleteQuestGM
================
*/
func (rt *Runtime) CompleteQuestGM(division, name, codename string) bool {
	return rt.gmQuest(division, name, "COMPQ", rt.GMQuests.Complete != nil, func(c *enterworld.Character) (GMQuestResult, error) {
		return rt.GMQuests.Complete(c, codename)
	})
}

/*
================
ResetQuestsGM
================
*/
func (rt *Runtime) ResetQuestsGM(division, name string) bool {
	return rt.gmQuest(division, name, "RESETQ", rt.GMQuests.Reset != nil, func(c *enterworld.Character) (GMQuestResult, error) {
		return rt.GMQuests.Reset(c)
	})
}

/*
================
gmQuest

Runs one tool on the GM's own character under the division lock, as the other
GM tools that change a character do, and delivers whatever it committed. A
refusal is logged with its reason and answered with the failure ack.
================
*/
func (rt *Runtime) gmQuest(division, name, command string, wired bool, run func(*enterworld.Character) (GMQuestResult, error)) bool {
	if rt == nil || rt.deps == nil || !wired {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || c.DeletePending || !c.GMPrivilege {
		return false
	}
	result, err := run(c)
	if len(result.Frames) > 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, result.Frames)
	}
	if len(result.Broadcast) > 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, result.Broadcast)
	}
	if err != nil {
		log.Infof("gmcommand: /%s refused for %s: %v", command, c.Name, err)
		return false
	}
	return true
}
