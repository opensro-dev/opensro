/*
===========================================================================

globalchat.go - the Global Chatting item: one message to the whole shard

CIFWholeChat (client CIFGlobalChatItem_OnSend 6D1EE0) sends the item's use
with its message: 0x75BD {u8 slot, u16 type word, u16 count + UTF-16LE
text} (CGInterface_SendGlobalChatItemUse 693C90). The reference server's
item use case 4 refuses a chat-restricted player
(its session restriction check, result 2), otherwise relays the
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
	"opensro.online/server/internal/game/social/chatfeed"
)

// globalChatRestricted is the native result 2 for a restricted player; the
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

The native item use case 4. The author's line follows its own 0xB5BD success; the
shard's copies go out once the use has committed. Natively the shard
manager relays the line asynchronously, so it never precedes the answer.
The line and the website transcript carry the author's display name
(enterworld.PartyMemberName: the job alias while suited, as on every
chat line), so what the site replays is what the shard saw; the peer
push still excludes the author by character name, the presence identity.
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
	display := enterworld.PartyMemberName(c)
	line := wire.Frame{Opcode: chat.OpChatBroadcast,
		Payload: chat.EncodeChatBroadcastNamed(chat.ChatTypeGlobal, display, message)}
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}, line}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	division, name := use.division, c.Name
	*after = func() {
		if rt.PushShardPeerFrames != nil {
			rt.PushShardPeerFrames(division, name, []wire.Frame{line})
		}
		rt.PublicChat.Record(division, chatfeed.KindGlobal, name, display, message)
	}
	return true
}

/*
================
PlayerBannedGM

/BAN's consequence for the website (gmcommand handleBan): the banned
character's lines leave the public chat feed. The feed matches the
character identity recorded on every line, so lines left under a job
alias go too, whatever alias the character has worn since. Port-only,
not native.
================
*/
func (rt *Runtime) PlayerBannedGM(division, name string) {
	rt.PublicChat.Purge(division, name)
}
