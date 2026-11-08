/*
===========================================================================

itemlinks_wire_test.go - a linked line's item references reach its readers

Port-only, not native (SRO_CHAT_ITEM_LINKS). A browser renders a linked
item only from references the server has sent it, and the world entry
preloads few of them (BUG-084, the drop notices' fix). So the references
of each linked item go out ahead of the line, to the same audience, and
ahead of the public history replayed to a player who enters later.

===========================================================================
*/
package chat_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
)

// linkRefOpcode stands for the item reference frame the action runtime
// sends (any opcode: the lane forwards the frames it is given).
const linkRefOpcode uint16 = 0x30AA

var linkRefFrame = wire.Frame{Opcode: linkRefOpcode, Payload: []byte{9, 9}}

// wireLinkSource links one bag item, slot 13, with one reference frame.
type wireLinkSource struct{}

func (wireLinkSource) LinkedItem(_ string, _ *enterworld.Character, ref chat.LinkRef) (chat.LinkedItem, bool) {
	if ref != (chat.LinkRef{Source: chat.LinkSourceInventory, Slot: 13}) {
		return chat.LinkedItem{}, false
	}
	return chat.LinkedItem{Body: []byte{1, 2, 3}, Label: "<X>", References: []wire.Frame{linkRefFrame}}, true
}

func (wireLinkSource) StorageLinks(string, *enterworld.Character, []chat.LinkRef) map[chat.LinkRef]chat.LinkedItem {
	return nil
}

/*
================
TestLinkedLineSendsItsReferencesFirst
================
*/
func TestLinkedLineSendsItsReferencesFirst(t *testing.T) {
	const division = "global-official"
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	server.chat.ItemLinks = &chat.ItemLinks{Source: wireLinkSource{}, Limit: 1}
	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)
	gameReadyBarrier(t, b, "empty history")

	request := append(chatRequestFrame(chat.ChatTypeAll, "", "wts \uFFFC"),
		chat.EncodeLinkRefs([]chat.LinkRef{{Source: chat.LinkSourceInventory, Slot: 13}})...)
	sendFrame(t, a, chat.OpChatRequest, request)
	line := chat.AppendLinkTail(namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "wts \uFFFC"),
		[]chat.LinkedItem{{Body: []byte{1, 2, 3}}})
	expectExact(t, a, linkRefOpcode, linkRefFrame.Payload, "the author's reference first")
	expectExact(t, a, chat.OpChatBroadcast, line, "the author's linked line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "receipt")
	expectExact(t, b, linkRefOpcode, linkRefFrame.Payload, "the reader's reference first")
	expectExact(t, b, chat.OpChatBroadcast, line, "the reader's linked line")

	c := dialWS(t, server.srv)
	helloWS(t, c)
	enterChatWorld(t, c, division, e2eChatNameC)
	expectExact(t, c, linkRefOpcode, linkRefFrame.Payload, "the replay's reference first")
	if history := expectFrame(t, c, chat.OpChatHistory, "the replayed linked line"); !bytes.HasSuffix(history, line) {
		t.Fatalf("history % X does not end with the linked line", history)
	}
}
