/*
===========================================================================

wire.go - the Contributor roster frame (port-only, not native, #3358)

0x3FC1 is a port-only S->C opcode of this build, the next free one after
the trade progress frame (0x3FC0); server wire and client dispatch were
both searched for it. Only this port's browser client consumes it, and a
client that predates it drops the unknown opcode.

Body, little-endian, the names in sorted order:

	u16 count
	count x { u16 length, length bytes of the character name }

The name bytes are the stored name, the bytes the spawn row's own name
field carries, so the client matches a nameplate or a chat sender
against the same text.

===========================================================================
*/
package contributor

import (
	"encoding/binary"
	"sort"
)

// OpRoster is the port-only S->C Contributor roster push.
const OpRoster uint16 = 0x3FC1

// OpPrompt is the port-only S->C login prompt (owner, 2026-11-14): one
// byte, the prompt's kind. The next free opcode after OpRoster; server
// wire and client dispatch were searched for 0x3FC2. A client that
// predates it drops the unknown opcode.
const OpPrompt uint16 = 0x3FC2

// The prompt kinds: the account lacks the title, or it carries it.
const (
	PromptElsewhere uint8 = 1
	PromptHolder    uint8 = 2
)

/*
================
Prompt

One session to ask, and which question.
================
*/
type Prompt struct {
	SessionID string
	Kind      uint8
}

// RosterMax bounds one frame; names past it are left out.
const RosterMax = 1024

// maxNameBytes bounds one name; a longer one is not a character name.
const maxNameBytes = 64

/*
================
EncodeRoster

Sorted and de-duplicated, so one roster always encodes to one payload
and Push can compare payloads to see a change.
================
*/
func EncodeRoster(names []string) []byte {
	sorted := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || len(name) > maxNameBytes || seen[name] {
			continue
		}
		seen[name] = true
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	if len(sorted) > RosterMax {
		sorted = sorted[:RosterMax]
	}
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(sorted)))
	for _, name := range sorted {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(name)))
		out = append(out, name...)
	}
	return out
}
