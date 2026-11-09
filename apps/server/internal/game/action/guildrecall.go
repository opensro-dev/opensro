/*
===========================================================================

guildrecall.go - the Guild Recall Scroll: summon the guild to the master

ITEM_ETC_GUILD_RECALL (type 3/3/12/3) is the expendable dispatch's case
3, beside the mercenary scroll (1) and the crest deletes (2). The
reference server's use runs its refusals in this order - no guild, a
world that is not a permanent type-0 world, not the master, the item's
guild-level floor, a busy master - then opens a timed window on the
master (the item's Param2 in milliseconds, read as seconds) and relays
the summon to every online member. The v1.150 client shows its
GuildSummonReq dialog from the 0x77C0 body (two dwords it stores
verbatim, plus the window in seconds); accepting or declining composes
0x7178 with those dwords and one accept byte. A decline never reaches
the later server's answer handler - only an accept does - and the
handler answers 0xB178: result 2 with the recall-error notice when the
member stands in a forbidden state, else the member is teleported to
the position the master stood at when the scroll was spent and result
1 closes the conversation. The window is the answer's whole lifetime:
an answer after it lapses finds nothing to answer.

Two port collapses, both recorded here because the native chain crosses
a shard relay the port does not run. The relay's two echo dwords (the
client only stores and returns them, refusing to answer while either
reads zero) are this server's guild id and the master's object id, and
the pending answers live in a window on the runtime where the native
keeps a timed job on the master. The GameWorld-side handler never sees
the accept byte (its relay arrives pre-filtered), so this lane reads
the byte itself and spends a decline on clearing the member's ask.
The native refusal checks whose exact state the ports cannot name -
the answer's five gates - are judged by their port equivalents:
a travel-block mask bit, the dead state, a teleport already in flight
or the criminal state, the battle state, and a dressed job. The use's
sixth native gate, one specific state id, has no port equivalent and
is not reproduced; the busy byte is the port's motion-change lock.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
guildRecallKey

One open summon, per guild in a division.
================
*/
type guildRecallKey struct {
	division string
	guildID  int64
}

/*
================
guildRecallWindow

An open summon: where the master stood when the scroll was spent, when
the window closes, and the members still asked. Members leave the map
as they answer; the window dies with its last answer or is replaced
once lapsed.
================
*/
type guildRecallWindow struct {
	masterID    int64
	masterEcho  uint32
	destination travelPoint
	expiresMs   int64
	pending     map[int64]struct{}
}

/*
================
guildRecallUse

A use the item-use closure admitted: the fan-out and the spend run
after that door closes (finishGuildRecallUse), the name-change
scroll's shape.
================
*/
type guildRecallUse struct {
	windowSeconds int64
	nowMs         int64
}

/*
================
guildRecallWindowSeconds

The answer window the scroll opens, in seconds: the reference reads
itemdata Param2 (column 2a0, milliseconds) divided by one thousand.
A scroll that names no positive window opens none.
================
*/
func guildRecallWindowSeconds(ref *enterworld.ItemRef) (int64, bool) {
	param, ok := ref.NativeFields.Lookup("itemParam2_2a0")
	if !ok {
		return 0, false
	}
	seconds := int64(param) / 1000
	return seconds, seconds > 0
}

/*
================
admitGuildRecallUse

The item-use closure's recall arm: shape only (empty tail, a guild to
summon, a window to open). The guild-side gates judge fresh store
state in the finish, so nothing is spent or asked here.
================
*/
func (rt *Runtime) admitGuildRecallUse(character *enterworld.Character, ref *enterworld.ItemRef, tail []byte, nowMs int64, result *OpResult) *guildRecallUse {
	if len(tail) != 0 {
		return nil
	}
	if character.GuildID == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall without a guild"
		return nil
	}
	seconds, ok := guildRecallWindowSeconds(ref)
	if !ok {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall scroll names no window"
		return nil
	}
	return &guildRecallUse{windowSeconds: seconds, nowMs: nowMs}
}

/*
================
finishGuildRecallUse

Judge the native refusal order against a fresh guild view - the
user's membership, the master's world, the mastership, the item's
level floor, a busy master, a recall already running - then spend
the scroll, open the window and ask every other member. A gate that
holds refuses and spends nothing. The fan-out pushes by name; a
member with no session simply hears nothing, as natively only the
online members are relayed to.
================
*/
func (rt *Runtime) finishGuildRecallUse(divisionID string, character *enterworld.Character, request wire.ItemUseRequest, ref *enterworld.ItemRef, use *guildRecallUse, result *OpResult) {
	authority := rt.deps.GuildAuthority()
	if authority == nil || character.GuildID == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall with no guild store wired"
		return
	}
	guildID := *character.GuildID
	record, members, ok := authority.Guild(divisionID, guildID)
	if !ok {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall in a guild the store does not hold"
		return
	}
	memberRow := -1
	for index, member := range members {
		if member.CharID == character.ID {
			memberRow = index
			break
		}
	}
	if memberRow < 0 {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall by a character the guild does not list"
		return
	}
	if here, found := instance.Lookup(instance.ID(domain.CharacterWorldInstance(character)).Definition()); !found || here.NativeType != 0 {
		*result = itemUseFailure(wire.ErrCodeGuildRecallRefused)
		return
	}
	if members[memberRow].Grade != domain.GuildLeaderGrade {
		*result = itemUseFailure(wire.ErrCodeGuildRecallNotMaster)
		return
	}
	if required, _ := ref.NativeFields.Lookup("itemParam18_2e0"); required > 0 && int64(record.Level) < int64(required) {
		*result = itemUseFailure(wire.ErrCodeGuildRecallNotMaster)
		return
	}
	if rt.PlayerAttackLocked(divisionID, character.Name) || rt.objectActionCommitted(divisionID, character.Name) {
		*result = itemUseFailure(wire.ErrCodeGuildRecallBusy)
		return
	}
	key := guildRecallKey{division: divisionID, guildID: guildID}
	if existing, loaded := rt.recallWindows.Load(key); loaded && use.nowMs < existing.(*guildRecallWindow).expiresMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return
	}
	destination := rt.standingPoint(divisionID, character)
	if destination.spawn.RegionID == 0 {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall from a master with no standing point"
		return
	}

	consumed := rt.deps.Update(character, "item-use-guild-recall", func() bool {
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.Slot) &&
				character.MissionInventory[index].Codename == ref.Codename {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		remaining := rt.consumeItemUseRow(character, rowIndex)
		*result = OpResult{Frames: []wire.Frame{
			{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
		}}
		result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
		return true
	})
	if !consumed {
		log.Warnf("action: guild recall for %q in %s passed its gates but its item row is gone; left unspent", character.Name, divisionID)
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: guild recall gates passed but the scroll row vanished before it could be spent"
		return
	}

	window := &guildRecallWindow{
		masterID:    character.ID,
		masterEcho:  enterworld.ObjectIDForCharacter(character),
		destination: destination,
		expiresMs:   use.nowMs + use.windowSeconds*1000,
		pending:     map[int64]struct{}{},
	}
	for _, member := range members {
		if member.CharID != character.ID {
			window.pending[member.CharID] = struct{}{}
		}
	}
	rt.recallWindows.Store(key, window)
	summon := wire.Frame{Opcode: guild.OpGuildRecallSummon,
		Payload: guild.EncodeGuildRecallSummon(uint32(guildID), window.masterEcho, uint32(use.windowSeconds))}
	if rt.PushCharacterFrames != nil {
		for _, member := range members {
			if member.CharID == character.ID || member.Name == "" {
				continue
			}
			rt.PushCharacterFrames(divisionID, member.Name, []wire.Frame{summon})
		}
	}
	rt.publishItemUseVisual(character, ref, 0, result)
}

/*
================
recallWindowFor

The open window still asking this member, if one does.
================
*/
func (rt *Runtime) recallWindowFor(divisionID string, characterID int64) (guildRecallKey, *guildRecallWindow, bool) {
	found := false
	var foundKey guildRecallKey
	var foundWindow *guildRecallWindow
	rt.recallWindows.Range(func(key, value any) bool {
		k := key.(guildRecallKey)
		if k.division != divisionID {
			return true
		}
		window := value.(*guildRecallWindow)
		if _, asked := window.pending[characterID]; asked {
			foundKey, foundWindow, found = k, window, true
			return false
		}
		return true
	})
	return foundKey, foundWindow, found
}

/*
================
closeRecallMember

Retire one member's ask; the window dies with its last one.
================
*/
func (rt *Runtime) closeRecallMember(key guildRecallKey, window *guildRecallWindow, characterID int64) {
	delete(window.pending, characterID)
	if len(window.pending) == 0 {
		rt.recallWindows.Delete(key)
	}
}

/*
================
guildRecallForbidden

The member states the native answer refuses, judged by their port
equivalents (see the file header): the travel-block mask, the dead
state, a teleport already in flight or the criminal state, the
battle state, and a dressed job.
================
*/
func (rt *Runtime) guildRecallForbidden(divisionID string, character *enterworld.Character, nowMs int64) bool {
	if rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(character)&0x20000 != 0 {
		return true
	}
	if !enterworld.CharacterAlive(character) {
		return true
	}
	if character.NativeTeleportMode != 0 || character.PVPState() == 2 {
		return true
	}
	if inBattleState(character, nowMs) {
		return true
	}
	return enterworld.DressedJob(character) != 0
}

/*
================
HandleGuildRecallAnswer

The 0x7178 answer: the echoed dwords must name the window this
member was asked in, or the answer is not one this server sent and
stays silent. A decline only retires the ask (the client already
closed its own dialog). An accept after the window lapsed closes
the dialog and moves nobody. A forbidden member hears the
recall-error notice; anyone else is teleported to where the master
stood when the scroll was spent, through the same relocation every
server-driven move uses, and hears the success byte.
================
*/
func (rt *Runtime) HandleGuildRecallAnswer(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	echoA, echoB, accept, ok := guild.DecodeGuildRecallAnswer(payload)
	if !ok || character == nil {
		return OpResult{}
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	key, window, found := rt.recallWindowFor(divisionID, character.ID)
	if !found {
		return OpResult{}
	}
	if echoA != uint32(key.guildID) || echoB != window.masterEcho {
		return OpResult{}
	}
	if !accept {
		rt.closeRecallMember(key, window, character.ID)
		return OpResult{}
	}

	nowMs := rt.Now().UnixMilli()
	expired := nowMs >= window.expiresMs
	destination := window.destination
	rt.closeRecallMember(key, window, character.ID)
	if expired {
		return OpResult{Frames: []wire.Frame{
			{Opcode: guild.OpGuildRecallResult, Payload: guild.EncodeGuildRecallClosed()},
		}}
	}
	refusal := OpResult{Frames: []wire.Frame{
		{Opcode: guild.OpGuildRecallResult, Payload: guild.EncodeGuildRecallRefused(wire.ErrCodeGuildRecallRefused)},
	}}
	if rt.guildRecallForbidden(divisionID, character, nowMs) {
		return refusal
	}
	moved := rt.relocateCharacter(divisionID, character, "guild-recall", func() (travelPoint, bool) {
		if !enterworld.CharacterAlive(character) || character.NativeTeleportMode != 0 {
			return travelPoint{}, false
		}
		return destination, true
	})
	if !moved {
		return refusal
	}
	return OpResult{Frames: []wire.Frame{
		{Opcode: guild.OpGuildRecallResult, Payload: guild.EncodeGuildRecallAccepted()},
	}}
}
