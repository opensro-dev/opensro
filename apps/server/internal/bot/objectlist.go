/*
===========================================================================

objectlist.go - parse the server object list into actors

The server sends one 0x3417 create row per actor between the 0x30CB start
and the 0x330A finalize. The bot reads the rows to find monsters. A row for
a known monster ref decodes as simulation.BuildMonsterCreateRow.

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
)

// OpcodeObjectListChunk is the 0x3417 actor create row.
const OpcodeObjectListChunk uint16 = 0x3417

// OpcodeObjectListStart is the 0x30CB object-list group start. A byListSub=2
// payload (first byte 0x02) opens a bulk despawn bracket: the following 0x3417
// chunk carries raw 4-byte gids, not spawn rows (issue #158).
const OpcodeObjectListStart uint16 = 0x30CB

// OpcodeObjectListFinalize is the 0x330A object-list group finalize.
const OpcodeObjectListFinalize uint16 = 0x330A

// Actor is one entity from the server object list.
type Actor struct {
	RefObjID uint32
	GID      uint32
	Region   uint16
	X        float64
	Y        float64
	Z        float64
	Name     string
}

// rowReader reads a native little-endian row.
type rowReader struct {
	data []byte
	pos  int
	err  error
}

func (r *rowReader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.pos+n > len(r.data) {
		r.err = fmt.Errorf("objectlist: short row at %d (need %d, have %d)", r.pos, n, len(r.data)-r.pos)
		return false
	}
	return true
}

func (r *rowReader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.data[r.pos]
	r.pos++
	return v
}

func (r *rowReader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v
}

func (r *rowReader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v
}

func (r *rowReader) f32() float64 {
	return float64(math.Float32frombits(r.u32()))
}

func (r *rowReader) bytes(n int) []byte {
	if n < 0 || !r.need(n) {
		return nil
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v
}

// ParseMonsterRow decodes one 0x3417 monster create row. The layout is
// simulation.BuildMonsterCreateRow (non-structure, non-trade-NPC).
func ParseMonsterRow(payload []byte) (Actor, error) {
	r := &rowReader{data: payload}
	actor := Actor{
		RefObjID: r.u32(),
		GID:      r.u32(),
		Region:   r.u16(),
		X:        r.f32(),
		Y:        r.f32(),
		Z:        r.f32(),
	}
	r.u16() // angle
	r.u8()  // pad
	r.u8()  // speed channel
	r.u8()  // pad
	r.u16() // angle
	r.u8()  // life state
	r.u8()  // motion state
	r.u8()  // pad
	r.f32() // walk speed
	r.f32() // run speed
	r.f32() // scale denominator
	skills := int(r.u8())
	for i := 0; i < skills; i++ {
		r.u32() // skill id
		r.u32() // token
	}
	r.u8() // name mask
	nameLen := int(r.u16())
	actor.Name = string(r.bytes(nameLen))
	r.u8() // rarity
	if r.err != nil {
		return Actor{}, r.err
	}
	return actor, nil
}

// MonsterRefs is a set of refObjIds the bot treats as monsters.
type MonsterRefs map[uint32]struct{}

// ParseRefs parses a comma-separated refObjId list. The value "all" or "*" (or
// an empty value) selects every monster: the caller then matches on the entity
// id band instead of a ref list (issue #163).
func ParseRefs(value string) (MonsterRefs, error) {
	refs := MonsterRefs{}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "*" || strings.EqualFold(trimmed, "all") {
		return refs, nil
	}
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bot: bad monster ref %q", part)
		}
		refs[uint32(id)] = struct{}{}
	}
	return refs, nil
}

// ActorFromRow returns the actor when the row names a monster. A non-empty ref
// set keeps the refObjId filter. An empty set (the "all" selection) accepts any
// row whose gid sits in the monster band, so the bot attacks every monster
// instead of only the configured refs (issue #163). The band keeps NPCs
// (200000+), players (100000+) and ground drops (300000+) out.
func ActorFromRow(payload []byte, refs MonsterRefs) (Actor, bool) {
	if len(payload) < 8 {
		return Actor{}, false
	}
	if len(refs) != 0 {
		if _, ok := refs[binary.LittleEndian.Uint32(payload)]; !ok {
			return Actor{}, false
		}
	} else if binary.LittleEndian.Uint32(payload[4:8]) < domain.MonsterGIDBase {
		return Actor{}, false
	}
	actor, err := ParseMonsterRow(payload)
	if err != nil {
		return Actor{}, false
	}
	return actor, true
}

// OpcodeObjectDespawn is the 0x36AB single-entity despawn.
const OpcodeObjectDespawn uint16 = 0x36AB

// DespawnGID returns the gid of a 0x36AB payload.
func DespawnGID(payload []byte) (uint32, bool) {
	if len(payload) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(payload), true
}

// DespawnGIDs parses a 0x3417 bulk-despawn chunk: a raw list of 4-byte gids,
// the byListSub=2 body the 0x30CB bracket opens (issue #158). A trailing
// partial gid is ignored.
func DespawnGIDs(payload []byte) []uint32 {
	out := make([]uint32, 0, len(payload)/4)
	for at := 0; at+4 <= len(payload); at += 4 {
		out = append(out, binary.LittleEndian.Uint32(payload[at:at+4]))
	}
	return out
}
