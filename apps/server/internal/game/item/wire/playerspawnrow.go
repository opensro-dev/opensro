package wire

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

// Peer character-appearance spawn row (the CICUser object row).
//
// This is the payload the client's remote-player spawn chain parses:
// The client's CPSMission_OnLevelUpEffect0x30D7 single mode (or a
// client's CPSMission_OnAck0x3417 object-list chunk) resolves the leading
// refObjId through the client's CIGIDObject_ResolveRefObj, classifies it
// CICUser at the client's GIDObject_SelectRuntimeClassAndSpawn user leaf
// (client 0x008514B2: char && tid1==4 && tid2==0x20 -> the client's
// CLASS_CICUSER class id (client 0xCF0108), constructs through the registered
// factory CICUser_Constructor / the chained
// CICUser_Constructor_Then_CICharactor_Constructor, then parses the row
// body through vtable+0x64, the client's
// CICUser_DeserializeAppearanceSpawnData:
//
//	vt+0x58 CICUser_DeserializeSpawnTail       (the two lead bytes)
//	the equipped-item loop                    (client 0x0086B00A..0x0086B07C)
//	the avatar/costume loop                   (client 0x0086B0C7..0x0086B12D)
//	the optional transform skin               (client 0x0086B14F..0x0086B1FC)
//	vt+0x5c CICPlayer_DeserializeSpawnData    (base
//	                                           CICharactor_DeserializeSpawnData + the
//	                                           name/job/ride/non-local tail)
//	vt+0x68 base CICharactor_DeserializeSpawnSubState appear byte
//	                                           (single-object mode only)
//
// Every field and gate below cites the client instruction that reads it
// (CICUser_DeserializeAppearanceSpawnData, CICPlayer_DeserializeSpawnData,
// CICharactor_DeserializeSpawnData, CICUser_DeserializeSpawnTail).

// Item TID-word classifiers the row layout depends on. These are 1:1 Go
// ports of the client's pure bitfield classifiers - the row SHAPE (which
// conditional bytes exist) is decided by the same predicates on both sides,
// so the ports carry the same per-branch native addresses.

// IsAvatarBandTid reports whether an equip-band item rides the avatar loop's
// conditional discard byte: the client's
// CICUser_DeserializeAppearanceSpawnData at client
// client 0x0086B0C7..0x0086B12D reads one u8
// and applies the avatar visual only when the item is equippable equipment
// AND its (typeBits & 0x780) group is 0x680 (the avatar/costume group).
func IsAvatarBandTid(typeFlags uint16) bool {
	return IsEquipmentBand(typeFlags) && typeFlags&0x780 == 0x680
}

// IsTwoHandGripTid ports the client's ItemTID_IsTraderSuit
// (client 5931B0): bit1 clear (client 0x005931B5), then the
// fully-specified TID (&0x1c)==0x0c, (&0x60)==0x20, (&0x780)==0x380,
// (&0xf800)==0x800 (client 0x005931B7..0x005931E5).
func IsTwoHandGripTid(typeFlags uint16) bool {
	if typeFlags&0x02 != 0 {
		return false
	}
	return typeFlags&0x1c == 0x0c &&
		typeFlags&0x60 == 0x20 &&
		typeFlags&0x780 == 0x380 &&
		typeFlags&0xf800 == 0x800
}

// IsJobUniformTid ports the client's ItemTID_IsThiefSuit (client 5931F0):
// bit1 clear (client 0x005931F5), then (&0x1c)==0x0c, (&0x60)==0x20,
// (&0x780)==0x380, (&0xf800)==0x1000 (client
// client 0x005931F7..0x0059322C).
func IsJobUniformTid(typeFlags uint16) bool {
	if typeFlags&0x02 != 0 {
		return false
	}
	return typeFlags&0x1c == 0x0c &&
		typeFlags&0x60 == 0x20 &&
		typeFlags&0x780 == 0x380 &&
		typeFlags&0xf800 == 0x1000
}

// IsBodyProtectorGroupTid ports the client's ItemTID_IsShield
// (client 5932C0): bit1 clear (client 0x005932C5), the 0x200 group gate
// (client 0x005932C7..0x005932E9), subclass (tid>>0xb) 1 or 2 (client
// client 0x005932EB..0x00593300).
func IsBodyProtectorGroupTid(typeFlags uint16) bool {
	if typeFlags&0x02 != 0 {
		return false
	}
	if typeFlags&0x1c == 0x0c && typeFlags&0x60 == 0x20 && typeFlags&0x780 == 0x200 {
		subclass := typeFlags >> 0xb
		return subclass == 1 || subclass == 2
	}
	return false
}

// interfaceEquipmentSlotSubtype is the native jump_table_868b90:
// (tid>>0xb) case 1..6 -> visual slot.
var interfaceEquipmentSlotSubtype = map[uint16]int{
	1: 0,
	2: 2,
	3: 1,
	4: 4,
	5: 3,
	6: 5,
}

// InterfaceEquipmentSlotForTid ports the client's
// ItemRecord_GetEquipSlotIndex (client 868A80): the visual
// avatar/equipment slot (0..8) an item TID word maps to, or -1. Slot 8 is
// the slot the weapon hold-type classification reads back (the client's
// CICUser_DeserializeAppearanceSpawnData at client 0x0086B08C via the
// client's CCObjCharacter_GetEquipVisualSlotItem).
func InterfaceEquipmentSlotForTid(typeFlags uint16) int {
	bit1 := typeFlags >> 1 & 1

	// Client 0x868AA8..0x868AE4: the wearable-gear category whose +0x780
	// nibble is
	// a real body slot and whose subtype (tid>>0xb) is in [1,6].
	if bit1 == 0 && typeFlags&0x1c == 0x0c && typeFlags&0x60 == 0x20 {
		nibble := typeFlags >> 7 & 0xf
		subtype := typeFlags >> 0xb
		isWearNibble := nibble == 1 || nibble == 2 || nibble == 3 ||
			nibble == 9 || nibble == 0xa || nibble == 0xb
		if isWearNibble {
			if slot, ok := interfaceEquipmentSlotSubtype[subtype]; ok {
				return slot
			}
		}
	}

	// Client 0x868B1B..0x868B46: the 0x300 group -> slot 6.
	if bit1 == 0 && typeFlags&0x1c == 0x0c && typeFlags&0x60 == 0x20 &&
		typeFlags&0x780 == 0x300 {
		return 6
	}

	// Client 0x868B52: the body/protector group (the client's
	// ItemTID_IsShield) -> slot 7.
	if IsBodyProtectorGroupTid(typeFlags) {
		return 7
	}

	// Client 0x868B5F..0x868B88: the 0x380 group -> slot 8.
	if bit1 == 0 && typeFlags&0x1c == 0x0c && typeFlags&0x60 == 0x20 &&
		typeFlags&0x780 == 0x380 {
		return 8
	}

	// Client 0x868B8E: no interface slot.
	return -1
}

// WeaponHoldTypeForTid ports the client's ItemRecord_GetJobSuitClass
// (client 868D00) for a resolved TID word. refObjID 0 is the
// "none / unarmed" code 4 (client 0x868D06); the record-resolve edge
// (client 0x868D13) is the caller's - the server passes the itemdata
// word it already holds.
func WeaponHoldTypeForTid(refObjID uint32, typeFlags uint16) uint8 {
	if refObjID == 0 {
		return 4
	}
	// Client 0x868D27..0x868D53: the specific 0x380/0x1800 family is
	// code 3.
	if typeFlags&0x02 == 0 &&
		typeFlags&0x1c == 0x0c &&
		typeFlags&0x60 == 0x20 &&
		typeFlags&0x780 == 0x380 &&
		typeFlags&0xf800 == 0x1800 {
		return 3
	}
	// Client 0x868D61: the two-hand-grip predicate short-circuits to
	// code 1.
	if IsTwoHandGripTid(typeFlags) {
		return 1
	}
	// Client 0x868D73..0x868D7A: `(!isJobUniform)*2 + 2` - job uniform 2,
	// else 4.
	if IsJobUniformTid(typeFlags) {
		return 2
	}
	return 4
}

// PlayerEquipItem is one worn (or avatar) item as the CICUser row carries
// it. TypeFlags is the itemdata TID word - the client re-derives it from its
// own record for refObjID, so the encoder MUST use the same word or the
// conditional bytes desync the stream.
type PlayerEquipItem struct {
	RefObjID  uint32
	TypeFlags uint16
	// OptLevel is the per-item enhancement level; on the wire only for
	// equip-band items (the client's CICUser_DeserializeAppearanceSpawnData
	// at client 0x0086B05C reads it after the slot map).
	OptLevel uint8
}

// PlayerSpawnRow is one remote-player (CICUser) entity row.
//
// It rides either the single-object spawn 0x30D7 or an object-list chunk
// 0x3417; the single-object path additionally reads the one-byte appear flag
// through the entity's vtable +0x68 (base
// CICharactor_DeserializeSpawnSubState) that the list path
// skips, so callers select it with WithAppearTail (the GroundItemRow
// convention).
type PlayerSpawnRow struct {
	// RefObjID is the character-model RefObjData id (the client's
	// CIGIDObject_ResolveRefObj target; its TID word must classify CICUser
	// at the client's GIDObject_SelectRuntimeClassAndSpawn leaf).
	RefObjID uint32
	// BodyShapeByte rides the vt+0x58 CICUser_DeserializeSpawnTail lead
	// byte into CICUser +0x758 (the harness mirror names it
	// packedBodyShape758).
	BodyShapeByte uint8
	// VisualFlags is the second vt+0x58 byte. CICUser_DeserializeSpawnTail
	// stages it in the shared client data_cf0100 cell and
	// CICUser_DeserializeAppearanceSpawnData applies it through the
	// client's CICUser_SetGMFlags to CICUser+0x779 after deserialization.
	// It is not a disposable zero byte.
	VisualFlags uint8
	// Skin is the transform the player wears (RefObjID 0: none).
	Skin TransformSkin
	// Equipment is the worn set for the client's 0x0086B00A equip loop,
	// in wire
	// order; later slot-8 items overwrite earlier ones in the client's
	// visual table, so order is contract.
	Equipment []PlayerEquipItem
	// Avatars is the avatar/costume set for the client's 0x0086B0C7 loop.
	Avatars []PlayerEquipItem
	Gid     uint32
	// Position is the client's CIGIDObject_DeserializeSpawnData base
	// block (gid/region/xyz/heading); the same heading word also rides
	// the packed-movement tail (mode-0 arm of the client's
	// MovementPayload_ReadFromStream, output+0x0a).
	Position
	// WalkSpeed/RunSpeed feed CICharactor +0x24c/+0x250 (the client's
	// CICharactor_DeserializeSpawnData at client
	// client 0x0085FB8F/0x0085FB9A); ScaleDenom feeds +0x4d8, an IEEE-754 FLOAT
	// on the wire (fld dword at client 0x0085FBA5) the client turns into
	// 100.0/denom at +0x4dc.
	WalkSpeed  float32
	RunSpeed   float32
	ScaleDenom float32
	// SpawnSkills is the active effect list the client's
	// CICharactor_DeserializeSpawnData reads after the scale
	// (g_aSpawnBuffSkillIds): the peer's buffs, so a player who comes into
	// view already buffed shows them.
	SpawnSkills []SpawnSkillEntry
	// Name is the display name the client's CICPlayer_DeserializeSpawnData
	// reads at client 0x00869E77 (the client's
	// CMsgStreamBuffer_ReadAsciiString wire shape: u16 length + `length`
	// single bytes) into CICUser +0x108.
	Name string
	// JobType/JobGrade land in +0x782/+0x783 (client
	// client 0x00869F89/0x00869F97).
	JobType  uint8
	JobGrade uint8
	PVPState uint8
	// Nil means no active event. Team zero is a real team, not the default.
	EventTeam *uint8
	// GuildName rides the client's 0x0086A0CC wire string into CICUser
	// +0x7a8. Natively +0x7bc - the "guild-present gate" the BindGuild
	// leg tests at client 0x0086A242 - is that embedded wstring's SIZE
	// field (+0x7a8+0x14; the client's
	// CICUser_Constructor_Then_CICharactor_Constructor zeroes it and
	// seeds capacity 7 at +0x7c0), so a NON-EMPTY name is what arms the
	// client's call to CICPlayer_BindCompanionOwner (0x00869810) at
	// client 0x0086A261; an empty name keeps the leg unreached - the
	// native no-guild presentation, not a shortcut.
	GuildName string
	// GuildID / GuildGrantName / the three crest params ride the
	// client's 0x0086A12D guild-member sub-block (member-class-4 rows
	// only - the weaponHoldType gate below): u32 guild id, grant name,
	// then the dwords var_88/var_84/var_8c the client forwards to
	// BindGuild (client 0x0086A24A..0x0086A261) as crestParamA/B/C - the
	// crest icon filename params the client's
	// CGuildCrestManager_UpdateCharacterCrests formats as
	// G{prefix}_{guildId}_{crestParamA}.crb (client 0x833E42) and
	// A{prefix}_{crestParamB}_{crestParamC}.crb (client 0x833E75). B/C are the
	// alliance crest plane (the client globals data_ced398/data_ced394 on
	// the 0x32C4 path, default 0): the guild's union id and emblem.
	GuildID        uint32
	GuildGrantName string
	CrestParamA    uint32
	CrestParamB    uint32
	CrestParamC    uint32
	// FortSiegeAuthority is the guild sub-block's trailing team byte: the
	// client reads it in CICPlayer_DeserializeSpawnData at client
	// 0x0086A1B2 and stores it via the client's
	// CICPlayer_SetFortressWarOfficerTitle into CICPlayer+0x7e0 (the
	// six-mark gate needs it in {1,2}). Values are a bit-for-bit match
	// to the client's lookup_table_869bcc: 0 None, 1 Commander,
	// 2 DeputyCommander,
	// 4 FortressWarAdministrator, 8 ProductionAdministrator,
	// 16 TrainingAdministrator, 32 MilitaryEngineer.
	FortSiegeAuthority uint8
	// StallTitle is the title of the stall the player keeps (empty: none);
	// it sets the title mode to 4 and rides after the guild sub-block.
	StallTitle string
	// StallDecoration is that stall's booth item reference (zero: default).
	StallDecoration uint32
	// WithAppearTail selects the 0x30D7 single-object form, which appends
	// the appear byte the drop-in presentation reads (vt+0x68, the
	// client's CICharactor_DeserializeSpawnSubState).
	WithAppearTail bool
	AppearFlag     uint8
}

// spawnTitleModeStall is +0x461's stall value (client 0x0086A26D in
// CICPlayer_DeserializeSpawnData; the client's
// CICharactor_ResumeSpawnMovement reads the same byte).
const spawnTitleModeStall uint8 = 4

// weaponHoldType computes the hold-type code the client's vt+0xac write
// stores at +0x4f5 after the equip loop: the client's
// CICUser_DeserializeAppearanceSpawnData at client 0x0086B08C reads the
// visual slot-8 entry back (the client's
// CCObjCharacter_GetEquipVisualSlotItem) and classifies it via the
// client's ItemRecord_GetJobSuitClass. The member-class vcall (vt+0xa8,
// MONOMORPHIC CICharactor_GetActiveJobClass reading +0x4f5) then gates
// the guild-member sub-block in CICPlayer_DeserializeSpawnData at
// client 0x0086A12D on the
// value 4 - so the EMITTER must derive the same code from the same worn set
// or the sub-block desyncs the stream.
//
// Active v1.150 itemdata includes trader/thief/hunter suits and free-PvP
// capes in TID 3.1.7.*. Slot 8 and the conditional guild block are live.
func (p PlayerSpawnRow) weaponHoldType() uint8 {
	var slot8Ref uint32
	var slot8Tid uint16
	for _, item := range p.Equipment {
		if !IsEquipmentBand(item.TypeFlags) {
			continue
		}
		// Later writes overwrite earlier ones in the client's +0x44 visual
		// table (the client's CICharactor_SetEquipSlotVisual), so the LAST
		// slot-8 item wins.
		if InterfaceEquipmentSlotForTid(item.TypeFlags) == 8 {
			slot8Ref = item.RefObjID
			slot8Tid = item.TypeFlags
		}
	}
	return WeaponHoldTypeForTid(slot8Ref, slot8Tid)
}

// wireString appends the client's CMsgStreamBuffer_ReadAsciiString wire
// shape:
// u16 length + `length` single bytes (the client widens each byte to a
// wchar). Callers keep peer names in the single-byte range; a multi-byte
// UTF-8 name would widen byte-per-byte, which is the same honest limit the
// wave-3 harness `str()` helper documents.
func wireString(w *Writer, value string) {
	raw := []byte(value)
	w.U16(uint16(len(raw)))
	w.Bytes(raw)
}

// Encode returns the CICUser spawn row, byte-for-byte the layout the
// client's fold chain consumes.
func (p PlayerSpawnRow) Encode() []byte {
	w := NewWriter(128)

	// The client's CIGIDObject_ResolveRefObj target: the character-model
	// RefObjData id.
	w.U32(p.RefObjID)

	// vt+0x58, the client's CICUser_DeserializeSpawnTail at client
	// client 0x00869110: the +0x758 body byte, then the complete visual-flags
	// byte later applied to CICUser+0x779 by
	// CICUser_DeserializeAppearanceSpawnData.
	w.U8(p.BodyShapeByte)
	w.U8(p.VisualFlags)

	// Equip loop header (client 0x0086AFEE/0x0086AFFC): one discarded
	// byte + the
	// equipped count. The count is a u8; the worn set is the 13-slot
	// equipment band in practice, so the clamp is defensive - it keeps
	// the count and the emitted bodies agreeing no matter what a caller
	// hands in.
	equipment := p.Equipment
	if len(equipment) > 0xff {
		log.Warnf("wire: spawn row for %s clamped equipment from %d to 255 rows (u8 count)", p.Name, len(equipment))
		equipment = equipment[:0xff]
	}
	w.U8(0)
	w.U8(uint8(len(equipment)))
	for _, item := range equipment {
		w.U32(item.RefObjID)
		// Client 0x0086B02D..0x0086B05C: the optLevel byte exists only
		// for
		// equip-band items (bit1 clear, 0x1c==0xc, 0x60==0x20).
		if IsEquipmentBand(item.TypeFlags) {
			w.U8(item.OptLevel)
		}
	}

	// Avatar loop header (client 0x0086B0AB/0x0086B0B9): discarded byte
	// + count.
	// Same defensive u8-count clamp as the equip loop (worn avatars are
	// <=4 in practice).
	avatars := p.Avatars
	if len(avatars) > 0xff {
		log.Warnf("wire: spawn row for %s clamped avatars from %d to 255 rows (u8 count)", p.Name, len(avatars))
		avatars = avatars[:0xff]
	}
	w.U8(0)
	w.U8(uint8(len(avatars)))
	for _, item := range avatars {
		w.U32(item.RefObjID)
		// Client 0x0086B0C7..0x0086B12D: one discarded byte only for the
		// avatar-band (equip-band && (0x780)==0x680) items.
		if IsAvatarBandTid(item.TypeFlags) {
			w.U8(0)
		}
	}

	// Transform skin (client 0x0086B14F): u8 present, then the skin block
	// (the skin block the server writes), applied through
	// CICharactor_ApplyTransformationSkin (86B265).
	if p.Skin.RefObjID != 0 {
		p.Skin.write(w.U8(1))
	} else {
		w.U8(0)
	}

	// vt+0x5c CICPlayer_DeserializeSpawnData -> base
	// CICharactor_DeserializeSpawnData -> base
	// CIGIDObject_DeserializeSpawnData: gid, region, xyz floats, heading
	// word (the RegisterGID block).
	w.U32(p.Gid)
	w.U16(p.RegionID)
	w.F32(p.X)
	w.F32(p.Y)
	w.F32(p.Z)
	w.U16(p.Heading)

	// The client's MovementPayload_ReadFromStream packed movement
	// (readSecondFlag=1): mode 0 = no in-flight
	// destination at spawn (peer movement rides the 0x30E3 tick broadcast),
	// store-only +0x255 second flag 0, then the mode-0 arm's rotation flag
	// + packed heading word (output+0x0e/+0x0a).
	w.U8(0)
	w.U8(0)
	w.U8(0)
	w.U16(p.Heading)

	// CICharactor_DeserializeSpawnData scalar rows: +0x263/+0x264/+0x460
	// state bytes, initialized to 0 like the NPC create row. Client
	// 0x0085FFA1 (in CICharactor_OnSpawnInitialize) promotes +460 through
	// the body setter; the peer visibility owner replays nonzero runtime
	// body state after spawn. Next are walk/run speeds, the FLOAT scale
	// denominator (fld dword at client 0x0085FBA5), then the active
	// effect list.
	w.U8(0)
	w.U8(0)
	w.U8(0)
	w.F32(p.WalkSpeed)
	w.F32(p.RunSpeed)
	w.F32(p.ScaleDenom)
	writeSpawnSkills(w, p.SpawnSkills)

	// CICPlayer_DeserializeSpawnData at client 0x00869E77: display name
	// -> +0x108 (the client's CMsgStreamBuffer_ReadAsciiString). The
	// local-player-only +0x1898 label read (client 0x00869ECE) never
	// rides a remote row.
	wireString(w, p.Name)

	// Client 0x00869F89/0x00869F97: job type/grade -> +0x782/+0x783. The
	// local-player job exp/contribution/reward trio (client 0x00869FAC)
	// never rides a remote row.
	w.U8(p.JobType)
	w.U8(p.JobGrade)

	// Client 0x0086A018..0x0086A036: +0x4f4 appearance byte, ride state
	// (0 = not riding; the client's CICharactor_SetMountedActionState
	// returns !=1 so no mount ref follows), +0x781 byte.
	w.U8(p.PVPState)
	w.U8(0)
	w.U8(0)

	// Non-local block (client 0x0086A067..0x0086A317), always on a peer
	// row: +0x784, title mode (+0x461: 4 = a stall, whose title block
	// follows the guild sub-block at client 0x0086A26D), +0x4f6.
	w.U8(0)
	if p.StallTitle != "" {
		w.U8(spawnTitleModeStall)
	} else {
		w.U8(0)
	}
	w.U8(0)

	// Client 0x0086A0CC: guild name -> +0x7a8. Natively +0x7bc is this
	// embedded wstring's size field, so a non-empty name arms the
	// client's call to CICPlayer_BindCompanionOwner (0x00869810), gated
	// at client 0x0086A242; empty keeps it unreached (no guild).
	wireString(w, p.GuildName)

	// Client 0x0086A12D: the guild-member sub-block rides ONLY when the
	// client's member-class vcall reads 4 off +0x4f5 - the weapon hold
	// type the equip loop just derived. Replicated field-for-field from
	// the same worn set (see weaponHoldType).
	if p.weaponHoldType() == 4 {
		w.U32(p.GuildID)                // guild id -> BindGuild arg 2
		wireString(w, p.GuildGrantName) // grant/rank name (empty -> +0x78c untouched)
		w.U32(p.CrestParamA)            // var_88 -> BindGuild crestParamA
		w.U32(p.CrestParamB)            // var_84 -> BindGuild crestParamB
		w.U32(p.CrestParamC)            // var_8c -> BindGuild crestParamC
		w.U8(p.FortSiegeAuthority)      // team byte -> client CICPlayer_SetFortressWarOfficerTitle +0x7e0
	}

	// Client 0x0086A26D: a stall's [wstr title][u32 decoration] ->
	// +0x75C/+0x754.
	if p.StallTitle != "" {
		w.WStr(p.StallTitle).U32(p.StallDecoration)
	}

	// Client 0x0086A2FF: action-progress seconds -> +0x780 (the client's
	// CICPlayer_SetInteractionState).
	w.U8(0)
	// Client 0x0086A33C -> +7E1: inactive event team, matching local
	// EnterWorld.
	// The server writes FF when neither event manager has a team.
	team := uint8(0xff)
	if p.EventTeam != nil {
		team = *p.EventTeam
	}
	w.U8(team)

	// vt+0x68 base, the client's CICharactor_DeserializeSpawnSubState
	// appear byte (single-object mode only); 1 is the drop-in
	// presentation, the grounditem registry convention.
	if p.WithAppearTail {
		w.U8(p.AppearFlag)
	}

	return w.Payload()
}

// DecodePlayerSpawnRow parses a CICUser spawn row back. Like the client, the
// decoder cannot recover item TID words from the payload - it resolves them
// through tidByRef, the same words the encoder used (the client's own
// itemdata lookup on each refObjId). withAppearTail selects the 0x30D7 form.
func DecodePlayerSpawnRow(payload []byte, tidByRef map[uint32]uint16, withAppearTail bool) (PlayerSpawnRow, error) {
	return DecodePlayerSpawnRowWithSkills(payload, tidByRef, withAppearTail, nil)
}

/*
================
DecodePlayerSpawnRowWithSkills

DecodePlayerSpawnRow for a row that carries effects: an entry's token and
status bytes depend on its skill record, which skillShape answers. Without
it only an empty effect list decodes.
================
*/
func DecodePlayerSpawnRowWithSkills(payload []byte, tidByRef map[uint32]uint16, withAppearTail bool, skillShape SpawnSkillShape) (PlayerSpawnRow, error) {
	var out PlayerSpawnRow
	out.WithAppearTail = withAppearTail

	r := NewReader(payload)
	var err error

	if out.RefObjID, err = r.U32(); err != nil {
		return out, err
	}
	if out.BodyShapeByte, err = r.U8(); err != nil {
		return out, err
	}
	if _, err = r.U8(); err != nil { // client cf0100 scratch byte
		return out, err
	}

	readItems := func(avatarLoop bool) ([]PlayerEquipItem, error) {
		if _, err := r.U8(); err != nil { // discarded loop lead byte
			return nil, err
		}
		count, err := r.U8()
		if err != nil {
			return nil, err
		}
		items := make([]PlayerEquipItem, 0, count)
		for n := 0; n < int(count); n++ {
			ref, err := r.U32()
			if err != nil {
				return nil, err
			}
			item := PlayerEquipItem{RefObjID: ref, TypeFlags: tidByRef[ref]}
			if avatarLoop {
				if IsAvatarBandTid(item.TypeFlags) {
					if _, err := r.U8(); err != nil {
						return nil, err
					}
				}
			} else if IsEquipmentBand(item.TypeFlags) {
				if item.OptLevel, err = r.U8(); err != nil {
					return nil, err
				}
			}
			items = append(items, item)
		}
		return items, nil
	}

	if out.Equipment, err = readItems(false); err != nil {
		return out, err
	}
	if out.Avatars, err = readItems(true); err != nil {
		return out, err
	}

	hasJob, err := r.U8()
	if err != nil {
		return out, err
	}
	if hasJob != 0 {
		return out, fmt.Errorf("wire: player spawn row carries a job block, which the emitter never writes")
	}

	if out.Gid, err = r.U32(); err != nil {
		return out, err
	}
	position, err := readPosition(r)
	if err != nil {
		return out, err
	}
	out.Position = position

	// Packed movement (mode 0 arm) + the three state bytes.
	for i := 0; i < 3; i++ {
		if _, err = r.U8(); err != nil {
			return out, err
		}
	}
	if _, err = r.U16(); err != nil { // packed heading word
		return out, err
	}
	for i := 0; i < 3; i++ {
		if _, err = r.U8(); err != nil {
			return out, err
		}
	}
	if out.WalkSpeed, err = r.F32(); err != nil {
		return out, err
	}
	if out.RunSpeed, err = r.F32(); err != nil {
		return out, err
	}
	if out.ScaleDenom, err = r.F32(); err != nil {
		return out, err
	}
	if out.SpawnSkills, err = readSpawnSkills(r, skillShape); err != nil {
		return out, err
	}

	readString := func() (string, error) {
		length, err := r.U16()
		if err != nil {
			return "", err
		}
		raw, err := r.Bytes(int(length))
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	if out.Name, err = readString(); err != nil {
		return out, err
	}

	if out.JobType, err = r.U8(); err != nil {
		return out, err
	}
	if out.JobGrade, err = r.U8(); err != nil {
		return out, err
	}
	if out.PVPState, err = r.U8(); err != nil {
		return out, err
	}
	// Ride state, +0x781, then the non-local +0x784/title/+0x4f6.
	var titleMode uint8
	for i := 0; i < 5; i++ {
		value, err := r.U8()
		if err != nil {
			return out, err
		}
		if i == 3 {
			titleMode = value
		}
	}
	if out.GuildName, err = readString(); err != nil { // guild name
		return out, err
	}
	if out.weaponHoldType() == 4 {
		if out.GuildID, err = r.U32(); err != nil { // guild id
			return out, err
		}
		if out.GuildGrantName, err = readString(); err != nil { // grant name
			return out, err
		}
		if out.CrestParamA, err = r.U32(); err != nil { // var_88
			return out, err
		}
		if out.CrestParamB, err = r.U32(); err != nil { // var_84
			return out, err
		}
		if out.CrestParamC, err = r.U32(); err != nil { // var_8c
			return out, err
		}
		if out.FortSiegeAuthority, err = r.U8(); err != nil { // team byte -> +0x7e0
			return out, err
		}
	}
	if titleMode == spawnTitleModeStall {
		if out.StallTitle, err = r.WStr(); err != nil {
			return out, err
		}
		if out.StallDecoration, err = r.U32(); err != nil { // decoration
			return out, err
		}
	}
	if _, err = r.U8(); err != nil { // action progress
		return out, err
	}
	team, err := r.U8()
	if err != nil {
		return out, err
	}
	if team != 0xff {
		out.EventTeam = &team
	}
	if withAppearTail {
		if out.AppearFlag, err = r.U8(); err != nil {
			return out, err
		}
	}

	if err := r.Done(); err != nil {
		return out, err
	}
	return out, nil
}
