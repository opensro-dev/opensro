/*
===========================================================================

itemlinks.go - item links in player chat: the wire tails and the labels

Port-only, not native (SRO_CHAT_ITEM_LINKS, owner decision 2026-11-07):
the v1.150 client has no item links. A player's 0x7367 line may carry,
after its text, where each linked item sits ({source, slot}); the text
holds LinkMarker where the link goes. The server reads each item from the
sender's own state (ItemLinkSource) and appends the item bodies after the
broadcast text, so a reader sees the exact item and a client cannot forge
one. A line without links is byte-identical to the native one.

Request block:   [u8 count] count x {[u8 source] [u16 slot]}
Broadcast tail:  [u8 count] count x {[u16 bodyLen] [ItemBody.Encode()]}

Link i belongs to the i-th LinkMarker in the text.

===========================================================================
*/
package chat

import (
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// LinkMarker stands in the text where a link goes (U+FFFC, the object
// replacement character): one UTF-16 unit, so it fits the native cap.
const LinkMarker rune = 0xFFFC

// Link sources: where the sender holds the linked item.
const (
	// LinkSourceInventory is the bag and the worn equipment (inventory slot).
	LinkSourceInventory uint8 = 0
	// LinkSourceStorage is the account warehouse, only while it is open.
	LinkSourceStorage uint8 = 1
	// LinkSourcePet is the summoned COS bag.
	LinkSourcePet uint8 = 2
	// LinkSourceAvatar is a worn avatar (costume) item.
	LinkSourceAvatar uint8 = 3
)

// EnvChatItemLinks is the port-only switch; off (the default) is native.
const EnvChatItemLinks = "SRO_CHAT_ITEM_LINKS"

/*
================
ItemLinksFromEnv

Whether SRO_CHAT_ITEM_LINKS is on ("on", "1" or "true").
================
*/
func ItemLinksFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvChatItemLinks))) {
	case "on", "1", "true":
		return true
	}
	return false
}

// MaxLinksOnWire bounds a request block: the per-line limit tunable's
// maximum. A larger block can only come from another client.
const MaxLinksOnWire = 8

// LinkRef is one link as the client sends it: where the item is.
type LinkRef struct {
	Source uint8
	Slot   uint16
}

// LinkedItem is one link as the server read it: the item body readers
// present, the label text consumers (moderation log, website feed) store
// in place of the marker, and the item reference frames a reader needs to
// render the body. A browser holds only the references it was sent and the
// world entry preloads few (BUG-084, the drop notices' fix), so these go
// out ahead of the line to the same audience.
type LinkedItem struct {
	Body       []byte
	Label      string
	References []wire.Frame
}

// ItemLinkSource reads a linked item from the sender's own state.
// LinkedItem reads the character's own rows (bag, worn, costume, pet) and
// may run inside a character door on a snapshot. StorageLinks reads the
// warehouse refs through the store, which takes its own lock and knows
// only the live character: it runs before any door, with the live one.
type ItemLinkSource interface {
	LinkedItem(divisionID string, character *enterworld.Character, ref LinkRef) (LinkedItem, bool)
	StorageLinks(divisionID string, live *enterworld.Character, refs []LinkRef) map[LinkRef]LinkedItem
}

// ItemLinks is the switch on: where items are read and how many one line
// may carry. A nil *ItemLinks is the switch off (native).
type ItemLinks struct {
	Source ItemLinkSource
	Limit  int
}

/*
================
DecodeLinkRefs

Reads a request link block. A count above MaxLinksOnWire or a short block
is an error.
================
*/
func DecodeLinkRefs(reader *wire.Reader) ([]LinkRef, error) {
	count, err := reader.U8()
	if err != nil {
		return nil, err
	}
	if int(count) > MaxLinksOnWire {
		return nil, fmt.Errorf("chat: %d item links breach the wire bound %d", count, MaxLinksOnWire)
	}
	refs := make([]LinkRef, 0, count)
	for i := 0; i < int(count); i++ {
		source, err := reader.U8()
		if err != nil {
			return nil, err
		}
		slot, err := reader.U16()
		if err != nil {
			return nil, err
		}
		refs = append(refs, LinkRef{Source: source, Slot: slot})
	}
	return refs, nil
}

/*
================
EncodeLinkRefs

The request link block for refs.
================
*/
func EncodeLinkRefs(refs []LinkRef) []byte {
	writer := wire.NewWriter(1 + len(refs)*3)
	writer.U8(uint8(len(refs)))
	for _, ref := range refs {
		writer.U8(ref.Source)
		writer.U16(ref.Slot)
	}
	return writer.Payload()
}

/*
================
AppendLinkTail

Appends the broadcast link tail to an encoded 0x3667 body. Without items
the payload comes back unchanged, so an unlinked line stays native.
================
*/
func AppendLinkTail(payload []byte, items []LinkedItem) []byte {
	if len(items) == 0 {
		return payload
	}
	size := 1
	for _, item := range items {
		size += 2 + len(item.Body)
	}
	writer := wire.NewWriter(size)
	writer.U8(uint8(len(items)))
	for _, item := range items {
		writer.U16(uint16(len(item.Body)))
		writer.Bytes(item.Body)
	}
	out := make([]byte, 0, len(payload)+size)
	out = append(out, payload...)
	return append(out, writer.Payload()...)
}

/*
================
linkPrelude

The reference frames a line's linked items need, in link order.
================
*/
func linkPrelude(items []LinkedItem) []wire.Frame {
	var frames []wire.Frame
	for _, item := range items {
		frames = append(frames, item.References...)
	}
	return frames
}

/*
================
LabelText

The line as a text consumer stores it: each marker replaced, in order, by
its item's label.
================
*/
func LabelText(message string, items []LinkedItem) string {
	if len(items) == 0 {
		return message
	}
	var out strings.Builder
	next := 0
	for _, r := range message {
		if r == LinkMarker && next < len(items) {
			out.WriteString(items[next].Label)
			next++
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

/*
================
ResolveLinks

Reads the request's links from the sender's own state and fits the text to
what was read (spec decision 7: trim, never refuse). storage holds the
warehouse refs StorageLinks read before any door. Link i belongs to
marker i: refs are taken in order while fewer than Limit are kept and the
source can read them, a dropped ref's own marker goes, and so does every
marker past the refs, so a pasted stray marker never reaches a reader.
================
*/
func ResolveLinks(links *ItemLinks, divisionID string, sender *enterworld.Character, storage map[LinkRef]LinkedItem,
	message string, refs []LinkRef) (string, []LinkedItem) {
	read := map[int]LinkedItem{}
	for i, ref := range refs {
		if len(read) >= links.Limit {
			break
		}
		item, ok := storage[ref]
		if ref.Source != LinkSourceStorage {
			item, ok = links.Source.LinkedItem(divisionID, sender, ref)
		}
		if ok {
			read[i] = item
		}
	}
	var out strings.Builder
	var items []LinkedItem
	marker := 0
	for _, r := range message {
		if r == LinkMarker {
			item, ok := read[marker]
			marker++
			if !ok {
				continue
			}
			items = append(items, item)
		}
		out.WriteRune(r)
	}
	return out.String(), items
}
