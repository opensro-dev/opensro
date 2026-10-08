/*
===========================================================================

avatarbless.go - the smith's avatar magic option grant

The smith's 0x2F row sends 0x7338 [npc][0x80000000]; B338 lock 0x80000000
opens CIFGrantMagicAttributeWnd beside the inventory. Its confirm
(CIFGrantMagicAttributeWnd_OnConfirm 6EBB10) sends

	0x361A [u8 inventory slot][u16 len][ascii option codename]
	    -> 0x32D9 [1][1][u8 slot][item body] | [2][u8 code]

v1.188 is 0x74A9 -> 0x34AA (CGObjPC_HandleMagicOptionGrant74A9 5079C0);
the rule lives in item/alchemy/avatar.go.

INFERENCE: 5079C0 checks no NPC, because the GameServer only reaches it
through the talk session. The port asks for that session explicitly: the
selected NPC grants the 0x80000000 row and its function is open.

===========================================================================
*/

package action

import (
	"errors"
	"sort"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const (
	opAvatarMagicOptionAdd    uint16 = 0x361a
	opAvatarMagicOptionAddAck uint16 = 0x32d9
)

/*
================
AvatarMagicOptions

The enter-world list of the options each avatar part may receive at a
smith, ascending by part; the client's grant window lists these.
================
*/
func (rt *Runtime) AvatarMagicOptions() []enterworld.AvatarMagicOptionRow {
	if rt.Alchemy == nil {
		return nil
	}
	rows := make([]enterworld.AvatarMagicOptionRow, 0, len(rt.Alchemy.AvatarOptions))
	for part, names := range rt.Alchemy.AvatarOptions {
		rows = append(rows, enterworld.AvatarMagicOptionRow{Part: part, Options: append([]string(nil), names...)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Part < rows[j].Part })
	return rows
}

/*
================
registerAvatarBless
================
*/
func (rt *Runtime) registerAvatarBless(hub *transport.Hub) {
	hub.Handle(opAvatarMagicOptionAdd, func(session *transport.Session, opcode uint16, payload []byte) {
		character, division, bound := enterworld.SessionCharacter(rt.deps, session)
		if bound {
			sendFrames(session, rt.HandleAvatarBless(division, character, payload))
		}
	})
}

/*
================
decodeAvatarBless
================
*/
func decodeAvatarBless(payload []byte) (uint8, string, error) {
	r := wire.NewReader(payload)
	slot, err := r.U8()
	if err != nil {
		return 0, "", err
	}
	codename, err := r.Str()
	if err != nil {
		return 0, "", err
	}
	return slot, codename, r.Done()
}

/*
================
avatarBlessRefusal

0x32D9 [2][code]: the client shows category 0x20 notice code.
================
*/
func avatarBlessRefusal(code alchemy.Refusal) []wire.Frame {
	return []wire.Frame{{Opcode: opAvatarMagicOptionAddAck, Payload: []byte{2, byte(code)}}}
}

/*
================
avatarBlessSession

The smith whose magic option row the character opened (npcaction.go).
================
*/
func (rt *Runtime) avatarBlessSession(division string, character *enterworld.Character) bool {
	gid, ok := rt.Selected.Get(division, character.Name)
	if !ok || !rt.Selected.FunctionOpen(division, character.Name, gid) {
		return false
	}
	npc, ok := rt.npcForCurrentViewer(division, character, gid)
	return ok && npc.TalkFlags&simulation.NpcTalkFlagMagicOption != 0
}

/*
================
HandleAvatarBless

One grant over a detached bag, committed through the character door like
every alchemy plan.
================
*/
func (rt *Runtime) HandleAvatarBless(division string, character *enterworld.Character, payload []byte) []wire.Frame {
	if character == nil || rt.Alchemy == nil {
		return nil
	}
	slot, codename, err := decodeAvatarBless(payload)
	if err != nil {
		log.Debugf("avatarbless: malformed 0x361A from %s: %v", character.Name, err)
		return nil
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.avatarBlessSession(division, character) {
		log.Debugf("avatarbless: %s has no open smith magic option session", character.Name)
		return nil
	}
	var frames []wire.Frame
	committed := rt.deps.Update(character, "avatar-bless", func() bool {
		if character.DeletePending || !enterworld.CharacterAlive(character) {
			return false
		}
		before := invItemsFromBag(character)
		var result alchemy.Outcome
		result, err = rt.Alchemy.BlessAvatar(before, slot, codename, rt.AlchemyRoll)
		if err != nil {
			return false
		}
		payload := wire.NewWriter(4).U8(1).U8(1).U8(slot).Payload()
		for _, row := range result.Items {
			if row.Slot == slot {
				payload = append(payload, row.Body().Encode()...)
				break
			}
		}
		frames = []wire.Frame{{Opcode: opAvatarMagicOptionAddAck, Payload: payload}}
		character.MissionInventory = alchemyRows(character.MissionInventory, before, result.Items)
		return true
	})
	if committed {
		return frames
	}
	var refusal alchemy.Refusal
	if errors.As(err, &refusal) {
		return avatarBlessRefusal(refusal)
	}
	if err != nil {
		log.Warnf("avatarbless: grant for %s failed: %v", character.Name, err)
	}
	return avatarBlessRefusal(alchemy.AvatarFailed)
}
