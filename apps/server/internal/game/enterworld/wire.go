/*
===========================================================================

wire.go - projects authoritative game state into native protocol records

===========================================================================
*/
package enterworld

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
)

// Native SR opcodes the bootstrap packet sequence carries, values verbatim
// from server.mjs.
const (
	OpcodeResetClient         uint16 = 0x3369
	OpcodeMyCharacterData     uint16 = 0x379d
	OpcodeMyCharacterChunk    uint16 = 0x32b3
	OpcodeMyCharacterFlush    uint16 = 0x31db
	OpcodeServerClockGidLatch uint16 = 0x32a6
	OpcodeObjectListStart     uint16 = 0x30cb
	OpcodeObjectListChunk     uint16 = 0x3417
	OpcodeObjectListFinalize  uint16 = 0x330a
	OpcodeGameTime            uint16 = 0x31ad
	OpcodeVitalsUpdate        uint16 = 0x33a6
)

// Packet is one {nativeOpcode, payload} bootstrap packet. Payload marshals as
// a JSON number array exactly like the Node side (never base64, which is what
// encoding/json would do to a []byte).
/*
================
Packet
================
*/
type Packet struct {
	Scope        []domain.ObjectScopeChange `json:"-"`
	NativeOpcode uint16                     `json:"nativeOpcode"`
	Payload      []int                      `json:"payload"`
}

// NewPacket ports buildMissionBootstrapPacket.
/*
================
NewPacket
================
*/
func NewPacket(nativeOpcode uint16, payload []byte) Packet {
	body := make([]int, len(payload))
	for index, value := range payload {
		body[index] = int(value)
	}
	return Packet{NativeOpcode: nativeOpcode, Payload: body}
}

// writeWireString appends the NativePacketWriter.string layout: u16 length +
// UTF-8 bytes.
/*
================
writeWireString
================
*/
func writeWireString(w *wire.Writer, value string) {
	bytes := []byte(value)
	w.U16(uint16(len(bytes)))
	w.Bytes(bytes)
}

// ObjectIDForCharacter ports missionObjectIdForCharacter: player entity ids
// live in the 100000+ band.
/*
================
ObjectIDForCharacter
================
*/
func ObjectIDForCharacter(c *Character) uint32 {
	id := int64(0)
	if c != nil {
		id = c.ID
	}
	if id < 0 {
		id = 0
	}
	if id > 0x7fffffff {
		id = 0x7fffffff
	}
	return uint32(100000 + id)
}

// CosObjectIDForCharacter returns the stable one-active-COS identity for a
// persisted owner. IDs outside the reserved COS band fail closed instead of
// truncating two owners onto one entity GID.
/*
================
CosObjectIDForCharacter
================
*/
func CosObjectIDForCharacter(c *Character) (uint32, bool) {
	if c == nil || c.ID < 0 || uint64(c.ID) > uint64(domain.MaxCOSOwnerID) {
		return 0, false
	}
	return domain.COSGIDBase + uint32(c.ID), true
}

// MaxMagicOptionsPerItem is the client's MAX_MAGPARAM_PER_ITEM: the count
// byte is asserted <= 12 at parse (sub_78b1b0 @0x0078b224, soitem.cpp:0x13b
// int3 on fail), so emission clamps rather than hand a hand-edited store
// record the power to assert-crash the client.
const MaxMagicOptionsPerItem = 12

// AvatarCapacityDefault is the avatar block's capacity byte for a character
// with no persisted avatar inventory - the value the composer always emitted
// while the block was hardcoded empty (byte identity for every existing
// character).
const AvatarCapacityDefault int64 = 5

// BuildItemBody ports the inventory branches of
// CSOItem_ParseFromStream/sub_78c830. Equipment rows retain the historical
// 18-byte base. ETC rows carry a u16 stack count and, for TID 3.3.14.2 Gacha
// result cards, the native indexed two-u64 reward parameters.
/*
================
BuildItemBody
================
*/
func BuildItemBody(row WireItem) []byte {
	refObjID := row.RefObjID
	if refObjID == 0 {
		refObjID = 1
	}
	plus := row.Plus
	if plus < 0 {
		plus = 0
	}
	if plus > 0xff {
		plus = 0xff
	}
	durability := row.Durability
	if durability < 0 {
		durability = 0
	}
	magicOptions := row.MagicOptions
	if len(magicOptions) > MaxMagicOptionsPerItem {
		magicOptions = magicOptions[:MaxMagicOptionsPerItem]
	}
	quantity := row.StackCount
	if quantity < 1 {
		quantity = 1
	}
	if quantity > 0xffff {
		quantity = 0xffff
	}
	return wire.ItemBody{
		TradeOwner:        row.TradeOwner,
		RefObjID:          refObjID,
		TypeFlags:         row.TypeFlags,
		Plus:              uint8(plus),
		VarianceBits:      row.VarianceBits,
		Durability:        uint32(durability),
		Quantity:          uint16(quantity),
		MagicOptions:      magicOptions,
		TransformRefObjID: row.TransformRefObjID, Summon: domain.CloneCOS(row.Summon),
	}.Encode()
}

// AvatarWireItems resolves the character's avatar block emission inputs: the
// capacity byte and the worn avatar rows re-armed for the binary writer. A
// character without a persisted avatar inventory answers the byte-identical
// empty block the composer always emitted (AvatarCapacityDefault, no rows).
/*
================
AvatarWireItems
================
*/
func AvatarWireItems(character *Character) (int64, []WireItem) {
	if character == nil || character.AvatarInventory == nil {
		return AvatarCapacityDefault, nil
	}
	capacity := character.AvatarInventory.Capacity
	if capacity <= 0 {
		capacity = AvatarCapacityDefault
	}
	if capacity > 0xff {
		capacity = 0xff
	}
	rows := character.AvatarInventory.Rows
	if len(rows) > 0xff {
		rows = rows[:0xff]
	}
	return capacity, InventoryWireItems(rows)
}

// Both native clock packets sample the shared calendar, never a login epoch.
/*
================
BuildServerClockGidLatchPayload
================
*/
func BuildServerClockGidLatchPayload(objectID uint32) []byte {
	return wire.NewWriter(8).U32(objectID).Bytes(calendar.Current().Payload()).Payload()
}

/*
================
BuildGameTimePayload
================
*/
func BuildGameTimePayload() []byte { return calendar.Current().Payload() }

// BuildVitalsRefreshPayload ports buildV150VitalsRefreshPayload: 0x33A6 u32
// objectId, u16 stateFlags, u8 updateMask 0x03, u32 hp, u32 mp.
/*
================
BuildVitalsRefreshPayload
================
*/
func BuildVitalsRefreshPayload(c *Character) []byte {
	// Maxima are DERIVED (charactervitals/vitals.go); currents are persisted state,
	// clamped into [0, max] at emission only (the LEAVE policy: a moved
	// maximum never rewrites a stored current).
	currentHp := CurrentHP(c)
	currentMp := CurrentMP(c)
	writer := wire.NewWriter(15)
	writer.U32(ObjectIDForCharacter(c))
	writer.U16(0)
	writer.U8(0x03)
	writer.U32(uint32(currentHp))
	writer.U32(uint32(currentMp))
	return writer.Payload()
}

// BuildLocalPlayerEntryPayload ports buildV150LocalPlayerEntryPayload: the
// 0x32B3 char-data chunk. Field order/widths are byte-identical to the WIP
// sub_863880 CICPlayer_BindRecords reconstruction; see the Node builder for
// the per-field provenance comments. Bytes must not drift - REV-1 diffs this
// against the Node output.
/*
================
BuildLocalPlayerEntryPayload
================
*/
func BuildLocalPlayerEntryPayload(character *Character, entry *LocalPlayerEntry, eventGuideStateMask uint32, equipRoster []WireItem) []byte {
	if entry == nil || !validEntrySkills(entry.SpawnSkills) {
		return nil
	}
	modelRef := entry.ModelRef
	if modelRef == 0 {
		modelRef = 1907
	}
	startProfile := entry.StartProfile
	objectID := ObjectIDForCharacter(character)
	bodyShape := int64(0)
	if character != nil {
		bodyShape = coerceInt(character.BodyShapeByte, 0, 0xff, 0)
	}
	level := coerceInt(charLevel(character), 1, 140, 1)
	maxLevelSource := charMaxLevel(character)
	if maxLevelSource == nil {
		maxLevelSource = charLevel(character)
	}
	maxLevel := coerceInt(maxLevelSource, 1, 140, level)
	// 0x32B3 carries CURRENTS only (the maxima's sole channel is 0x343C);
	// the derived maxima serve as the emission clamp/fallback here.
	currentHp := CurrentHP(character)
	currentMp := CurrentMP(character)
	skillPoints := coerceInt(charSkillPoints(character), 0, 0x7fffffff, 0)
	statPoints := coerceInt(charStatPoints(character), 0, 0xffff, 0)
	gold := coerceInt(charGold(character), 0, 0x7fffffff, 0)
	experience := coerceInt(charExperience(character), 0, 0x7fffffff, 0)
	skillExp := coerceInt(charSkillExp(character), 0, 0x7fffffff, 0)
	movementMode := startProfile.MovementMode
	if movementMode != MovementModeWalk && movementMode != MovementModeRun {
		movementMode = MovementModeRun
	}
	name := ""
	if character != nil {
		name = character.Name
	}

	writer := wire.NewWriter(256)
	// Base runtime block (the 15-read wire segment).
	writer.U32(modelRef)
	writer.U8(uint8(bodyShape))
	writer.U8(uint8(level))
	writer.U8(uint8(maxLevel))
	writer.U64(uint64(experience))
	writer.U32(uint32(skillExp))
	writer.U64(uint64(gold))
	writer.U32(uint32(skillPoints))
	writer.U16(uint16(statPoints))
	writer.U8(min(character.BerserkPoints, 5)) // persistent Hwan gauge
	writer.U32(0)                              // wireValue844
	writer.U32(uint32(currentHp))
	writer.U32(uint32(currentMp))
	// data_cf0100 is misleadingly named in the decompiler: sub_8675f0
	// immediately feeds this byte to CICUser_ApplyStateFlags779. It is the
	// authoritative two-bit visual-flags seed, not an unrelated option byte.
	writer.U8(ResolveVisualFlags(character))
	var daily uint8
	var total uint16
	var penalty uint32
	if character != nil && character.PK != nil {
		daily, total, penalty = character.PK.DailyCount, character.PK.TotalCount, character.PK.Penalty
	}
	writer.U8(daily)
	writer.U16(total)
	writer.U32(penalty)

	// Inventory block (sub_8675f0 step 2): u8 size, u8 count, then per item
	// u8 slot + the CSOItem body bytes. The size is the character's capacity
	// byte, the only place the v1.150 client learns it (CICPlayer+0x1848).
	// The count is a u8; the clamp is unreachable through the equip-roster
	// deriver (capacity is at most 77) and only guards a hand-edited store
	// record from emitting a count that disagrees with the bodies.
	if len(equipRoster) > 0xff {
		log.Warnf("bootstrap: char-data equip roster for %s clamped from %d to 255 rows (u8 count)", name, len(equipRoster))
		equipRoster = equipRoster[:0xff]
	}
	writer.U8(inventory.BagEnd(character))
	writer.U8(uint8(len(equipRoster)))
	for _, item := range equipRoster {
		slot := item.Slot
		if slot < 0 || slot > 0xff {
			slot = 6
		}
		writer.U8(uint8(slot))
		writer.Bytes(BuildItemBody(item))
	}

	// Avatar block (sub_8675f0 ~L451-482): u8 capacity (native reads and
	// discards it at 0x8677a9; the WIP fold retains it as
	// avatarCapacityWire8677a9) + u8 count + count x [u8 slot + full
	// CSOItem body]. Rows come from the persisted avatar inventory
	// (Character.AvatarInventory - see its field doc for the content
	// constraints); an avatar-less record emits the same [5][0] bytes as
	// before. Every emitted refObjId rides the refItemSnapshot with its
	// full record (buildRefItemSnapshot walks the avatar rows), because an
	// unseeded ref DESYNCS the client stream (sub_78c830 L182-183).
	avatarCapacity, avatarRoster := AvatarWireItems(character)
	// AvatarWireItems already caps its rows at 0xff; this clamp is the
	// defensive count-site twin so the u8 count can never disagree with
	// the emitted bodies even if that deriver changes.
	if len(avatarRoster) > 0xff {
		log.Warnf("bootstrap: char-data avatar roster for %s clamped from %d to 255 rows (u8 count)", name, len(avatarRoster))
		avatarRoster = avatarRoster[:0xff]
	}
	writer.U8(uint8(avatarCapacity))
	writer.U8(uint8(len(avatarRoster)))
	for _, item := range avatarRoster {
		slot := item.Slot
		if slot < 0 || slot > 0xff {
			slot = 0
		}
		writer.U8(uint8(slot))
		writer.Bytes(BuildItemBody(item))
	}
	// Mastery list (sub_866e50): u8 counter, then per row a marker byte 1
	// + u32 masterydata id + u8 level, terminated by marker 2. Rows come
	// from the persisted character masteries (schema v4: the racial set,
	// seeded at level 1); a mastery-less record emits the same empty
	// [0][2] bytes as before.
	// The count is a u8; masteries are capped at the racial set (<=7) so
	// the clamp only guards a hand-edited store record.
	masteries := character.Masteries
	if len(masteries) > 0xff {
		log.Warnf("bootstrap: char-data mastery list for %s clamped from %d to 255 rows (u8 count)", name, len(masteries))
		masteries = masteries[:0xff]
	}
	writer.U8(uint8(len(masteries)))
	for _, mastery := range masteries {
		writer.U8(1)
		writer.U32(mastery.ID)
		writer.U8(uint8(mastery.Level))
	}
	writer.U8(2)
	// Skill list (sub_866e50's second loop, same marker framing): per row
	// u32 skilldata id + one value byte. Rows come from the persisted
	// learned skills (schema v5); the value byte is 1, the native entry
	// ctor's own default (sub_866e50 @0x00866e81 seeds value=1 before the
	// read) and what the learn ack path inserts (sub_8509f0 @0x00850a35).
	// A skill-less record emits the same empty [0][2] bytes as before.
	// The count is a u8 and skill learning has NO cap, so a long-lived
	// character can genuinely cross 255 learned skills - without the
	// clamp the wrapped count byte disagrees with the emitted bodies and
	// the client stream desyncs. Clamp BEFORE both, loudly.
	skills := character.Skills
	if len(skills) > 0xff {
		log.Warnf("bootstrap: char-data skill list for %s clamped from %d to 255 rows (u8 count) - the tail is NOT emitted", name, len(skills))
		skills = skills[:0xff]
	}
	writer.U8(uint8(len(skills)))
	for _, skillID := range skills {
		writer.U8(1)
		writer.U32(skillID)
		writer.U8(1)
	}
	writer.U8(2)
	// Quest block (sub_8673d0's three counted sections). Section 1 -
	// COMPLETED quest refs: u8 count + count x u32 questdata id LE, read
	// into the CICPlayer +0x18d0 container's completed list (ids are
	// v1.150-native questdata rows - see the CompletedQuestIds field
	// doc). An empty list emits the same single zero byte as before.
	completedQuests := character.CompletedQuestIds
	if len(completedQuests) > 0xff {
		completedQuests = completedQuests[:0xff]
	}
	writer.U8(uint8(len(completedQuests)))
	for _, questID := range completedQuests {
		writer.U32(questID)
	}
	// Section 2 - ACTIVE quests: u8 count + count x [u32 refId +
	// SQuestInfo body] (client parse sub_788210; field emission is
	// FLAG-DRIVEN, see the ActiveQuestRecord doc - a flags/field mismatch
	// desyncs the stream). An empty list emits the same single zero byte
	// the composer hardcoded before this section was modeled.
	activeQuests := character.ActiveQuests
	if len(activeQuests) > 0xff {
		activeQuests = activeQuests[:0xff]
	}
	writer.U8(uint8(len(activeQuests)))
	for _, quest := range activeQuests {
		writer.U32(quest.RefID)
		writer.U8(quest.U08)
		writer.U8(quest.U09)
		writer.U8(quest.Flags)
		if quest.Flags&0x04 != 0 {
			writer.U32(quest.Progress)
		}
		if quest.Flags&0x08 != 0 {
			writer.U8(quest.U10)
		}
		if quest.Flags&0x10 != 0 {
			contents := quest.Contents
			if len(contents) > 0xff {
				contents = contents[:0xff]
			}
			writer.U8(uint8(len(contents)))
			for _, node := range contents {
				writer.U8(node.Tag)
				writer.U8(node.Kind)
				writeWireString(writer, node.Description)
				if node.ObjectiveSentinel {
					// The 0xFF count sentinel: NO value array bytes
					// (sub_785c60 @0x785d03 wrap).
					writer.U8(0xff)
					continue
				}
				values := node.ObjectiveValues
				// 0xFF would read as the sentinel; a well-formed record
				// never approaches it (retail objectives are 1-2 values).
				if len(values) > 0xfe {
					values = values[:0xfe]
				}
				writer.U8(uint8(len(values)))
				for _, value := range values {
					writer.U32(value)
				}
			}
		}
		if quest.Flags&0x40 != 0 {
			targets := quest.TargetIds
			if len(targets) > 0xff {
				targets = targets[:0xff]
			}
			writer.U8(uint8(len(targets)))
			for _, targetID := range targets {
				writer.U32(targetID)
			}
		}
	}
	// Section 3 - TRACKED quest records: u8 count + count x [u32 refId,
	// u8 flags, u8 valueA, u16 word, 6 tail bytes, u32 optional iff
	// flags&0x02] (client sub_8673d0 @0x8674a0..0x867524; applied by
	// sub_866c60 - the browser client's hosted apply drives the CIFQuest
	// +0x398 tracked id the minimap quest pass gates on). An empty list
	// emits the same single zero byte as before.
	trackedQuests := character.TrackedQuests
	if len(trackedQuests) > 0xff {
		trackedQuests = trackedQuests[:0xff]
	}
	writer.U8(uint8(len(trackedQuests)))
	for _, record := range trackedQuests {
		writer.U32(record.RefID)
		writer.U8(record.Flags)
		writer.U8(record.ValueA)
		writer.U16(record.Word)
		for index := 0; index < 6; index++ {
			var tailByte uint8
			if index < len(record.Tail6) {
				tailByte = record.Tail6[index]
			}
			writer.U8(tailByte)
		}
		if record.Flags&0x02 != 0 {
			writer.U32(record.Optional)
		}
	}

	// Position + movement + speeds + scale.
	writer.U32(objectID)
	writer.U16(uint16(startProfile.RegionID))
	writer.F32(float32(startProfile.X))
	writer.F32(float32(startProfile.Y))
	writer.F32(float32(startProfile.Z))
	writer.U16(uint16(startProfile.Angle))
	writer.U8(0)
	writer.U8(uint8(movementMode))
	writer.U8(0)
	writer.U16(0)
	writer.U8(0)
	writer.U8(0)
	writer.U8(0)
	walk, run := entry.WalkSpeed, entry.RunSpeed
	if walk <= 0 {
		walk = 20
	}
	if run <= 0 {
		run = 50
	}
	writer.F32(walk) // +0x24c walk-speed source
	writer.F32(run)  // +0x250 run-speed source
	actionSpeed := entry.ActionSpeed
	if actionSpeed <= 0 {
		actionSpeed = 100
	}
	writer.F32(actionSpeed) // +0x4d8 action-speed denominator
	writer.U8(uint8(len(entry.SpawnSkills)))
	for _, skill := range entry.SpawnSkills {
		writer.U32(skill.ID)
		if skill.Token != nil {
			writer.U32(*skill.Token)
			writer.U32(*skill.Remaining)
		}
		if skill.HasStatus {
			writer.U8(skill.Status)
		}
	}
	writeWireString(writer, name)
	writeWireString(writer, "")
	writer.U8(0)
	writer.U8(0)
	writer.U32(0)
	writer.U32(0)
	writer.U32(0)
	writer.U8(character.PVPState())
	writer.U8(0)
	writer.U8(0)
	// CICUser+7E1 is event membership, independent of guild-war affiliation.
	writer.U8(character.EventTeam())
	writer.U32(eventGuideStateMask)
	// CICPlayer+0x1894 is the character JID used by ground-drop ownership and
	// party item sharing. It is the same world/member identity as the local
	// object GID, not a database row id.
	writer.U32(objectID)
	// Privilege byte -> CICPlayer+0x1890: the local full-character
	// deserialize sub_8675f0 reads u32 +0x1894 (the JID dword above,
	// @0x867a80) then THIS u8 (@0x867a90) - the position is byte-critical.
	// Bit 0 is the v1.150 GM privilege flag, tested by sub_862a70
	// ("movzx eax, byte [ecx+0x1890]; and eax, 1"); its consumers are
	// Console_IsAllowed @0x004fa670 (console/GM-command availability -
	// bit 0 OR the retail session byte +0x12f), the GM default chat
	// channel mode 3 in CIFChatViewer_HandleChatInputKey @0x006aed90,
	// and the 30F2/33C4/3647 mission handlers. Bit 1 enables the PC-room
	// event interface (862A80 -> 683B40 -> 547750(1)); other bits stay 0.
	var privilegeByte1890 uint8
	if character.GMPrivilege {
		privilegeByte1890 |= 0x01
	}
	if character.PCRoomEvent {
		privilegeByte1890 |= 0x02
	}
	writer.U8(privilegeByte1890)
	writer.Bytes(buildQuickSlotHudStatePayload(character))
	writer.U16(character.AutoPotion.HP)
	writer.U16(character.AutoPotion.MP)
	writer.U16(character.AutoPotion.Cure)
	writer.U8(character.AutoPotion.Timing)
	// Chunk C of the entered stream: the WHISPER-BLOCK list (client fold
	// sub_77ad10 between the auto-potion chunk above and the fortress-war
	// id below - the insert position is byte-critical, one moved read
	// desyncs the whole OnMyCharacterEntered parse). Wire: u8 count, then
	// per name u16 len + ANSI bytes (stream.readString). The persisted
	// list is capped at the mutate site (WhisperBlockMaxCount = the
	// client panel's 0x14); the emission clamp here only guards a
	// hand-edited store record from overflowing the count byte.
	blocked := character.BlockedWhisperers
	if len(blocked) > 0xff {
		blocked = blocked[:0xff]
	}
	writer.U8(uint8(len(blocked)))
	for _, name := range blocked {
		writeWireString(writer, name)
	}
	// "No fortress war" sentinel 0x10001 (a literal 0 reads as war id 0 and
	// closes the world-map follow gate).
	writer.U32(entryFortressWorld(entry))
	// Event-group list (client fold sub_77b220 @0x0077b2f4..): u8 count +
	// count x u32 groupId LE. Every id sets the client's event-start slot;
	// id 1 additionally fires the render-option pair and the
	// UIIT_MSG_EVENT_START guide message. Ids are POLICY content (declared
	// choice - see the EnterEventGroupIds field doc); an empty list emits
	// the same single zero byte as before.
	eventGroups := character.EnterEventGroupIds
	if len(eventGroups) > 0xff {
		eventGroups = eventGroups[:0xff]
	}
	writer.U8(uint8(len(eventGroups)))
	for _, groupID := range eventGroups {
		writer.U32(groupID)
	}
	// Mission-mode byte: native reads and discards it (the WIP fold retains
	// it diagnostically); no modeled state warrants a field, so it stays 0.
	writer.U8(0)
	return writer.Payload()
}

// buildQuickSlotHudStatePayload renders the exact sub_778460 block:
// mode 7, u8 count, then count x {u8 slot, u8 kind, u32 payload}. Persisted
// rows are normalized by slot so a hand-edited duplicate cannot produce two
// competing writes to one CIFUnderBar control during reveal.
/*
================
buildQuickSlotHudStatePayload
================
*/
func buildQuickSlotHudStatePayload(character *Character) []byte {
	rowsBySlot := make(map[uint8]QuickSlotBinding, domain.QuickSlotCount)
	if character != nil {
		for _, row := range character.QuickSlots {
			if int(row.Slot) >= domain.QuickSlotCount || !QuickSlotKindValid(row.Kind) {
				log.Warnf(
					"bootstrap: discarded invalid quickslot row for %s (slot=%d kind=0x%02X)",
					character.Name,
					row.Slot,
					row.Kind,
				)
				continue
			}
			if row.Kind == 0 {
				delete(rowsBySlot, row.Slot)
				continue
			}
			rowsBySlot[row.Slot] = row
		}
	}

	writer := wire.NewWriter(2 + len(rowsBySlot)*6)
	writer.U8(7)
	writer.U8(uint8(len(rowsBySlot)))
	for slot := 0; slot < domain.QuickSlotCount; slot++ {
		row, ok := rowsBySlot[uint8(slot)]
		if !ok {
			continue
		}
		writer.U8(row.Slot)
		writer.U8(row.Kind)
		writer.U32(row.Payload)
	}
	return writer.Payload()
}

/*
================
charLevel
================
*/
func charLevel(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.Level
}

/*
================
charMaxLevel
================
*/
func charMaxLevel(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.MaxLevel
}

/*
================
charSkillPoints
================
*/
func charSkillPoints(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.SkillPoints
}

/*
================
charStatPoints
================
*/
func charStatPoints(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.StatPoints
}

/*
================
charGold
================
*/
func charGold(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.Gold
}

/*
================
charExperience
================
*/
func charExperience(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.Experience
}

/*
================
charSkillExp
================
*/
func charSkillExp(c *Character) *int64 {
	if c == nil {
		return nil
	}
	return c.SkillExp
}

/*
================
PersistentCOSObjectID

Separate owner-scoped bands prevent a horse, attack pet and pickup pet from
sharing a world identity. References and inventory slots are not actor IDs.
================
*/
func PersistentCOSObjectID(c *Character, band uint16) (uint32, bool) {
	if c == nil || c.ID <= 0 || uint64(c.ID) > uint64(domain.MaxCOSOwnerID) {
		return 0, false
	}
	switch band {
	case 3:
		return domain.AttackPetGIDBase + uint32(c.ID), true
	case 4:
		return domain.PickupPetGIDBase + uint32(c.ID), true
	case domain.CapturedCOSBand:
		return domain.CapturedCOSGIDBase + uint32(c.ID), true
	default:
		return 0, false
	}
}
