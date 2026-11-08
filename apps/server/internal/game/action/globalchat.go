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
	"opensro.online/server/internal/game/social/chatlog"
)

// globalChatRestricted is the native result 2 for a restricted player; the
// restriction notice has already told them why.
const globalChatRestricted uint8 = 2

// globalChatTypeIDs is the Global Chatting item (native item use case 4,
// itemuse_admission.go admittedItemUseFamily).
var globalChatTypeIDs = [4]int64{3, 3, 3, 5}

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
	// storage holds the line's warehouse links, read before the door
	// (globalChatStorageLinks); port-only, SRO_CHAT_ITEM_LINKS.
	storage map[chat.LinkRef]chat.LinkedItem
}

/*
================
Runtime.globalChatStorageLinks

Port-only, not native (SRO_CHAT_ITEM_LINKS): a Global Chatting use's
warehouse links, read before the item-use door. The door holds the store's
lock and the warehouse read takes it too, so reading inside would hang the
store (chatitemlinks.go StorageLinks).
================
*/
func (rt *Runtime) globalChatStorageLinks(divisionID string, live *enterworld.Character, request wire.ItemUseRequest, tail []byte) map[chat.LinkRef]chat.LinkedItem {
	global := enterworld.ItemRef{TypeIDs: globalChatTypeIDs}
	if rt.ChatItemLinks == nil || request.TypeWord != global.TypeFlags() {
		return nil
	}
	_, refs, ok := decodeGlobalChatMessage(tail, true)
	if !ok {
		return nil
	}
	return rt.StorageLinks(divisionID, live, refs)
}

/*
================
decodeGlobalChatMessage

The 0x75BD tail: u16 wide-char count + UTF-16LE text, nothing after. The
client's edit box holds at most 0x64 characters (CIFWholeChat_OnCreate
6D2080) and never sends an empty line (client 6D1EE0 answers it locally), so a
longer, empty or trailing tail can only come from another client. With links
on (port-only, not native: SRO_CHAT_ITEM_LINKS), the chat lane's item-link
block may follow the text (chat/itemlinks.go).
================
*/
func decodeGlobalChatMessage(tail []byte, links bool) (string, []chat.LinkRef, bool) {
	reader := wire.NewReader(tail)
	count, err := reader.U16()
	if err != nil || count == 0 || count > chat.ChatMessageMaxWideChars {
		return "", nil, false
	}
	text, err := reader.Bytes(int(count) * 2)
	if err != nil {
		return "", nil, false
	}
	var refs []chat.LinkRef
	if links && reader.Done() != nil {
		if refs, err = chat.DecodeLinkRefs(reader); err != nil {
			return "", nil, false
		}
	}
	if reader.Done() != nil {
		return "", nil, false
	}
	units := make([]uint16, count)
	for i := range units {
		units[i] = uint16(text[i*2]) | uint16(text[i*2+1])<<8
	}
	return string(utf16.Decode(units)), refs, true
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
	message, refs, ok := decodeGlobalChatMessage(tail, rt.ChatItemLinks != nil)
	if !ok {
		return false
	}
	c := use.character
	if rt.ChatRestricted != nil && rt.ChatRestricted(use.division, c.Name) {
		*result = itemUseFailure(globalChatRestricted)
		return false
	}
	// Port-only, not native: the links are read before the item is spent,
	// so a link to the last Global Chatting item shows it as it was.
	var items []chat.LinkedItem
	if rt.ChatItemLinks != nil {
		message, items = chat.ResolveLinks(rt.ChatItemLinks, use.division, c, use.storage, message, refs)
	}
	text := chat.LabelText(message, items)
	remaining := rt.consumeItemUseRow(c, use.row)
	display := enterworld.PartyMemberName(c)
	line := wire.Frame{Opcode: chat.OpChatBroadcast,
		Payload: chat.AppendLinkTail(chat.EncodeChatBroadcastNamed(chat.ChatTypeGlobal, display, message), items)}
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}, line}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	division, name := use.division, c.Name
	// Port-only, not native: the staff moderation log (chatlog) keeps
	// the line, muted ones included and marked. It is a publication like
	// the shard push and the website line below, so it waits for the
	// post-commit step: a door that rolls the use back must not keep a
	// line that was never said, for an item that was never spent.
	muted := rt.ChatShadowMuted != nil && rt.ChatShadowMuted(c.AccountID)
	record := func() {
		if rt.ModerationLog != nil {
			rt.ModerationLog.Record(chatlog.Entry{
				AccountID: c.AccountID, Character: c.Name, Channel: "global", Muted: muted, Text: text,
			})
		}
	}
	if muted {
		// The item is spent and the author sees the line; nobody else does.
		*after = record
		return true
	}
	*after = func() {
		record()
		if rt.PushShardPeerFrames != nil {
			// A linked item's references go ahead of the line (BUG-084).
			var prelude []wire.Frame
			for _, item := range items {
				prelude = append(prelude, item.References...)
			}
			rt.PushShardPeerFrames(division, name, append(prelude, line))
		}
		rt.PublicChat.Record(division, chatfeed.KindGlobal, name, display, text)
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
