/*
===========================================================================

globalchat.go - the Global Chatting item: one message to the whole shard

CIFWholeChat (client CIFGlobalChatItem_OnSend 6D1EE0) sends the item's use
with its message: 0x75BD {u8 slot, u16 type word, u16 count + UTF-16LE
text} (CGInterface_SendGlobalChatItemUse 693C90). CGItemExpendable_UseItem
(v1.188 49B9F0) case 4 refuses a chat-restricted player
(CGObjPC_CheckAndReportSessionRestriction, result 2), otherwise relays the
line to the shard manager as a type 6 chat {name, message} and reports
success, which spends one item. The shard sends it to every player.

===========================================================================
*/

package action

import (
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
)

// globalChatRestricted is 49B9F0's result 2 for a restricted player; the
// restriction notice has already told them why.
const globalChatRestricted uint8 = 2

/*
================
globalChatUse

One Global Chatting use, inside the character's door.
================
*/
type globalChatUse struct {
	division  string
	character *enterworld.Character
	row       int
	request   wire.ItemUseRequest
}

/*
================
decodeGlobalChatMessage

The 0x75BD tail: u16 wide-char count + UTF-16LE text, nothing after. The
client's edit box holds at most 0x64 characters (CIFWholeChat_OnCreate
6D2080) and never sends an empty line (6D1EE0 answers it locally), so a
longer, empty or trailing tail can only come from another client.
================
*/
func decodeGlobalChatMessage(tail []byte) (string, bool) {
	reader := wire.NewReader(tail)
	count, err := reader.U16()
	if err != nil || count == 0 || count > chat.ChatMessageMaxWideChars {
		return "", false
	}
	text, err := reader.Bytes(int(count) * 2)
	if err != nil || reader.Done() != nil {
		return "", false
	}
	units := make([]uint16, count)
	for i := range units {
		units[i] = uint16(text[i*2]) | uint16(text[i*2+1])<<8
	}
	return string(utf16.Decode(units)), true
}

/*
================
useGlobalChatting

49B9F0 case 4. The author's line follows its own 0xB5BD success; the
shard's copies go out once the use has committed. Natively the shard
manager relays the line asynchronously, so it never precedes the answer.
================
*/
func (rt *Runtime) useGlobalChatting(use globalChatUse, tail []byte, result *OpResult, after *func()) bool {
	message, ok := decodeGlobalChatMessage(tail)
	if !ok {
		return false
	}
	c := use.character
	if rt.ChatRestricted != nil && rt.ChatRestricted(use.division, c.Name) {
		*result = itemUseFailure(globalChatRestricted)
		return false
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	line := wire.Frame{Opcode: chat.OpChatBroadcast,
		Payload: chat.EncodeChatBroadcastNamed(chat.ChatTypeGlobal, c.Name, message)}
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}, line}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	division, name := use.division, c.Name
	*after = func() {
		if rt.PushShardPeerFrames != nil {
			rt.PushShardPeerFrames(division, name, []wire.Frame{line})
		}
	}
	return true
}
