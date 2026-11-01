/*
===========================================================================

e2e_chardata_test.go - the 0x32B3 char-data parser for the progression e2e lane

Split from e2e_wire_test.go on its own responsibility boundary: the wire
scenarios there drive sessions, this file owns reading the char-data chunk
in BuildLocalPlayerEntryPayload's field order, including the native marker
grammar for the mastery and learned-skill lists (#1480).

===========================================================================
*/

package progression_test

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
==================
charData

charData is the subset of the 0x32B3 chunk this lane asserts, parsed
with BuildLocalPlayerEntryPayload's exact field order (internal/game/enterworld/
wire.go; itself byte-pinned to the native readers
CICPlayer_DeserializeBaseRecord and SkillList_DeserializeLearned).
==================
*/
type charData struct {
	Level       uint8
	MaxLevel    uint8
	Exp         uint64
	SkillExp    uint32
	SkillPoints uint32
	StatPoints  uint16
	CurrentHP   uint32
	CurrentMP   uint32
	Masteries   map[uint32]uint8
	Skills      []uint32
	// The 0x32B3 quest block's three section id lists
	// (CICPlayer_DeserializeQuestSections):
	// walked so a populated section cannot hide behind the skill pins.
	CompletedQuests []uint32
	ActiveQuests    []uint32
	TrackedQuests   []uint32
}

/*
================
parseCharData
================
*/
func parseCharData(t *testing.T, payload []byte) charData {
	t.Helper()
	r := wire.NewReader(payload)
	fail := func(what string, err error) {
		if err != nil {
			t.Fatalf("0x32B3 parse: %s: %v", what, err)
		}
	}
	skip := func(n int, what string) {
		_, err := r.Bytes(n)
		fail(what, err)
	}

	// Base runtime block, in writer order: modelRef u32, bodyShape u8,
	// level u8, maxLevel u8, exp u64, skillExp u32, gold u64. The
	// level/exp fields are parsed (not skipped) since the levelup wave:
	// the levelling e2e asserts the persisted walk through them.
	skip(4+1, "modelRef + bodyShape")
	out := charData{Masteries: map[uint32]uint8{}}
	var err error
	out.Level, err = r.U8()
	fail("level", err)
	out.MaxLevel, err = r.U8()
	fail("maxLevel", err)
	out.Exp, err = r.U64()
	fail("exp", err)
	out.SkillExp, err = r.U32()
	fail("skillExp", err)
	skip(8, "gold")
	out.SkillPoints, err = r.U32()
	fail("skillPoints", err)
	out.StatPoints, err = r.U16()
	fail("statPoints", err)
	skip(1+4, "berserk + wireValue844")
	out.CurrentHP, err = r.U32()
	fail("currentHp", err)
	out.CurrentMP, err = r.U32()
	fail("currentMp", err)
	skip(1+1+2+4, "pk block")

	// Inventory block: u8 capacity, u8 count, per item u8 slot + the
	// VARIABLE-length CSOItem equipment body (17 fixed bytes + u8
	// magicParamCount + count x u64 - BuildEquipmentItemBody /
	// CSOItem_DeserializeMagicOptions).
	skipItemBody := func(what string) {
		skip(4+1+8+4, what+" body head")
		magicCount, err := r.U8()
		fail(what+" magic count", err)
		skip(int(magicCount)*8, what+" magic params")
	}
	skip(1, "inventory capacity")
	invCount, err := r.U8()
	fail("inventory count", err)
	for i := 0; i < int(invCount); i++ {
		skip(1, "inventory slot")
		skipItemBody("inventory item")
	}

	// Avatar block: u8 capacity + u8 count + count x [u8 slot + the same
	// variable CSOItem body] (CICPlayer_DeserializeEnteredInventoryAndInitializeHud's
	// avatar loop).
	skip(1, "avatar capacity")
	avatarCount, err := r.U8()
	fail("avatar count", err)
	for i := 0; i < int(avatarCount); i++ {
		skip(1, "avatar slot")
		skipItemBody("avatar item")
	}

	// Mastery list: the lead byte is discarded by the native reader,
	// which takes marker-1 rows until the marker-2 terminator
	// (SkillList_DeserializeLearned's first loop) - there is no count
	// and no 255 cap, so past 255 rows the lead byte wraps unread (#1480).
	_, err = r.U8()
	fail("mastery lead byte", err)
	for row := 0; ; row++ {
		marker, err := r.U8()
		fail("mastery marker", err)
		if marker == 2 {
			break
		}
		if marker != 1 {
			t.Fatalf("0x32B3 mastery row %d marker = %d, want 1 or the terminator 2", row, marker)
		}
		id, err := r.U32()
		fail("mastery id", err)
		level, err := r.U8()
		fail("mastery level", err)
		out.Masteries[id] = level
	}

	// Skill list: the same marker grammar (SkillList_DeserializeLearned's
	// second loop):
	// lead byte discarded, per row marker 1 + u32 id + u8 value(1),
	// until marker 2.
	_, err = r.U8()
	fail("skill lead byte", err)
	for row := 0; ; row++ {
		marker, err := r.U8()
		fail("skill marker", err)
		if marker == 2 {
			break
		}
		if marker != 1 {
			t.Fatalf("0x32B3 skill row %d marker = %d, want 1 or the terminator 2", row, marker)
		}
		id, err := r.U32()
		fail("skill id", err)
		value, err := r.U8()
		fail("skill value", err)
		if value != 1 {
			t.Fatalf("0x32B3 skill row %d value byte = %d, want the native ctor default 1", row, value)
		}
		out.Skills = append(out.Skills, id)
	}

	// Quest block (CICPlayer_DeserializeQuestSections' three counted
	// sections; the composer
	// emits all three for real since the 2026-07-29 quest wave, so this
	// parser walks the full grammar instead of stopping blind after the
	// skills - a populated section cannot hide from the e2e assertions).
	// Section 1: completed refs.
	completedCount, err := r.U8()
	fail("completed quest count", err)
	for i := 0; i < int(completedCount); i++ {
		id, err := r.U32()
		fail("completed quest id", err)
		out.CompletedQuests = append(out.CompletedQuests, id)
	}
	// Section 2: active SQuestInfo records (the flag-gated body of
	// SQuestInfo_Deserialize).
	activeCount, err := r.U8()
	fail("active quest count", err)
	for i := 0; i < int(activeCount); i++ {
		id, err := r.U32()
		fail("active quest refId", err)
		out.ActiveQuests = append(out.ActiveQuests, id)
		skip(2, "active quest u08+u09")
		flags, err := r.U8()
		fail("active quest flags", err)
		if flags&0x04 != 0 {
			skip(4, "active quest progress")
		}
		if flags&0x08 != 0 {
			skip(1, "active quest u10")
		}
		if flags&0x10 != 0 {
			contentsCount, err := r.U8()
			fail("active quest contents count", err)
			for node := 0; node < int(contentsCount); node++ {
				skip(2, "contents tag+kind")
				strLen, err := r.U16()
				fail("contents string length", err)
				skip(int(strLen), "contents string")
				objectiveCount, err := r.U8()
				fail("contents objective count", err)
				if objectiveCount != 0xff {
					skip(int(objectiveCount)*4, "contents objective values")
				}
			}
		}
		if flags&0x40 != 0 {
			targetCount, err := r.U8()
			fail("active quest target count", err)
			skip(int(targetCount)*4, "active quest target ids")
		}
	}
	// Section 3: tracked records (optional u32 iff flags&0x02).
	trackedCount, err := r.U8()
	fail("tracked quest count", err)
	for i := 0; i < int(trackedCount); i++ {
		id, err := r.U32()
		fail("tracked quest refId", err)
		out.TrackedQuests = append(out.TrackedQuests, id)
		flags, err := r.U8()
		fail("tracked quest flags", err)
		skip(1+2+6, "tracked quest valueA+word+tail")
		if flags&0x02 != 0 {
			skip(4, "tracked quest optional")
		}
	}
	return out
}
