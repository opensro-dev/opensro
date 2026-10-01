/*
===========================================================================

opcodes.go - inventory and item opcodes and error codes

Package wire implements the typed encoders and decoders for the
Silkroad Online v1.150 inventory/item wire surface.

Every layout in this package is pinned to an address in the SRO_Client.exe
v1.150 decompile. Where a field order or width is easy to get
wrong the constructor doc comment names the instruction address so the next
reader can check it without re-deriving the layout.

The framework's network.Packet is a fixed 4096-byte buffer whose readers
bound-check against the buffer rather than the received payload, so decoding
here operates on plain []byte payloads and rejects both short and
over-long input. Encoders return []byte; the Packet helpers in packet.go
wrap them with the right MessageID for handlers that speak the framework.

===========================================================================
*/
package wire

// Opcodes carried by the inventory/item plane. Direction is from the server's
// point of view: Request opcodes arrive from the client, the rest are sent.
const (
	// OpItemMoveRequest is the client's item-operation request.
	// Composer sub_699250, serializer sub_697e80.
	OpItemMoveRequest uint16 = 0x706D
	// OpItemMoveResponse is the server's item-operation result row.
	// Parser sub_759a30, handler sub_75a3d0.
	OpItemMoveResponse uint16 = 0xB06D
	// OpTargetInteract is the multiplexed target/action request. A CIItem click
	// serializes the pickup execute in sub_698740; sub_693190's one-byte form
	// is a throttled cancel/recovery command, not an execute heartbeat.
	OpTargetInteract uint16 = 0x72CD
	// OpItemUseRequest is the selected inventory-item use request composed by
	// sub_6961b0. The common potion body is [u8 slot][u16 type word].
	OpItemUseRequest uint16 = 0x75BD
	// OpItemUseResponse is the item-use result parsed by sub_755e40. Its
	// successful potion arm drives cooldown bookkeeping, SND_POTION, and the
	// inventory stack replacement in that order.
	OpItemUseResponse uint16 = 0xB5BD
	// OpPickupAnim triggers the pickup scoop animation. Handler sub_7780f0.
	OpPickupAnim uint16 = 0x35C7
	// OpObjectDespawn removes a single entity. Handler sub_777310.
	OpObjectDespawn uint16 = 0x36AB
	// OpSingleObjectSpawn spawns one entity outside an object list.
	// Handler sub_7772f0 -> sub_777220 single mode.
	OpSingleObjectSpawn uint16 = 0x30D7
	// OpGroundOwnershipExpired clears CIItem's temporary owner reservation.
	// v1.150 payload is exactly the ground entity GID dword (client 0x31E2).
	OpGroundOwnershipExpired uint16 = 0x31E2
	// OpObjectListChunk carries object-list entity rows. Handler sub_77bdc0.
	OpObjectListChunk uint16 = 0x3417
	// OpGoldRefresh carries the character's gold balance. Handler sub_777720.
	OpGoldRefresh uint16 = 0x3126
	// OpObjectSourceMove repositions a remote entity. Handler sub_775cb0.
	OpObjectSourceMove uint16 = 0x30E3
	// OpObjectSourceCorrection hard-corrects a remote entity's position.
	// Handler sub_775b50.
	OpObjectSourceCorrection uint16 = 0xB2F5
	// OpObjectStateRefresh flips one discrete state channel on an entity.
	// Handler sub_777b60.
	OpObjectStateRefresh uint16 = 0x3122
	// OpLocalRebirthRequest is CIFMessageBox kind 3's self-rebirth choice.
	// sub_6971f0 composes a one-byte body at 0x00697278/0x00697346:
	// 1 = the specified rebirth point, 2 = the present point (level <= 10).
	OpLocalRebirthRequest uint16 = 0x32DC
	// OpRebirthPointAppointRequest designates the selected teleport guide's
	// town return point. Response is OpRebirthPointAppointResult.
	OpRebirthPointAppointRequest uint16 = 0x720D
	OpRebirthPointAppointResult  uint16 = 0xB20D
)

// Item movement types, the discriminator byte shared by the 0x706D request and
// the 0xB06D result (sub_697e80 / sub_759a30 dispatch on the same value).
const (
	// MoveTypeInventory moves between bag and equipment slots.
	MoveTypeInventory uint8 = 0x00
	// MoveTypePickup grants a ground item (or gold) into the bag.
	MoveTypePickup uint8 = 0x06
	// MoveTypeGroundDrop drops a bag item onto the ground.
	MoveTypeGroundDrop uint8 = 0x07
	// MoveTypeGoldDrop drops gold onto the ground.
	MoveTypeGoldDrop uint8 = 0x0A
)

// Result byte values leading a 0xB06D payload.
const (
	ResultSuccess uint8 = 0x01
	ResultError   uint8 = 0x02
)

// PickupGoldSlot is the sentinel slot a type-0x06 grant carries when the
// pickup was gold rather than an item; the remainder is then a u32 amount
// instead of a CSOItem body.
const PickupGoldSlot uint8 = 0xFE

// Native error codes for a failed 0xB06D ([0x02][code]).
//
// The client maps [0x02][code] through sub_75a3d0 -> sub_689420 /
// CGInterface_ShowSystemNotice as notice category 0x01; the per-code UIIT
// keys below are read off that dispatcher's recovered jump table (the
// sub_689420 WIP fold). The retired Node fixture collapsed every storage
// refusal into 0x07; the per-cause codes replaced that in the
// clothes/hard-armor wave so the player reads the retail reason instead of
// a wrong "inventory full".
const (
	// ErrCodeInvalidRequest is the generic rejection: unsupported movement
	// type, a character pending deletion, a missing reference-data row, or a
	// request the retail client cannot compose at all (slot out of range,
	// empty source slot). The client notice table has no 01:02 entry, so the
	// refusal is silent - matching the native client, which never sends
	// these shapes.
	ErrCodeInvalidRequest uint8 = 0x02
	// ErrCodeStorageFull is UIIT_MSG_STRGERR_INVENTORY_FULL (01:07): the bag
	// genuinely has no free slot.
	ErrCodeStorageFull uint8 = 0x07
	// ErrCodeCantEquip is UIIT_MSG_STRGERR_EQUIPITEM (01:0e, "Cannot equipt
	// the selected item"): the item cannot sit in the requested socket
	// (non-equipable item, wrong socket class). INFERRED from the client
	// notice table; the official GameServer byte for this cause is
	// unverified. Reachability verdict (dump-audited): LIVE player-facing -
	// the bag->equip composer (sub_594980/sub_699250) does not socket-check,
	// and the equip<->equip and unequip-swap-back legs reach this refusal
	// from real UI drags. The client also self-raises the SAME string
	// (guide-only, no 0xB06D) on its avatar/itemmall pre-checks
	// (@0x00594b63, @0x006ca826), so the copy is already familiar to
	// players and notice-consistent for the server path.
	ErrCodeCantEquip uint8 = 0x0E
	// ErrCodeNotEnoughGold is UIIT_MSG_STRGERR_NOT_ENOUGH_GOLD (01:0f,
	// banner + guide): the request exceeds the carried balance.
	ErrCodeNotEnoughGold uint8 = 0x0F
	// ErrCodeNotEnoughHonor is UIIT_MSG_TC_LACK_HONOR_POINT (01:D4,
	// CGInterface_ShowSystemNotification 689CDE: table index 0xD1 + 3): an
	// honor-priced package the character's honor points cannot pay.
	ErrCodeNotEnoughHonor uint8 = 0xD4
	// ErrCodeLevelRequired is UIIT_MSG_STRGERR_HIGHER_LEVEL_REQUIRED
	// (01:10, banner + guide): the character level is below the item's
	// RequiredLevel floor (native full-mask sub_789c60 bit 0x020, level
	// byte player+0x820 vs the type-1 ReqLevel quad). NOTE: the retired Node fixture once
	// misused this byte as "unknown character"; that use is gone - an item
	// op without a bound character answers the silent generic
	// ErrCodeInvalidRequest, and 0x10 now carries exactly the meaning the
	// client notice table always gave it. (The enter-world/bootstrap
	// envelope's NativeErrorCode 0x10 = UIO_MSG_ERROR_ID is a DIFFERENT
	// channel and keeps its meaning there.)
	ErrCodeLevelRequired uint8 = 0x10
	// v1.150 client 01:6C, 689555: HIGHER_LEVEL_REQUIRED_TO_USE_THISITEM.
	// Client notice contract; not a claim about later-server error numbering.
	ErrCodeItemUseLevelRequired uint8 = 0x6C
	// Research server 510CD0/510DA5/4FCF74 returns 1889/185B/185C.
	// v1.150 category 1 retains these low-byte meanings; its reply uses u8.
	ErrCodeItemUseDead uint8 = 0x89
	// ErrCodeCosTarget is the low byte of 49D240's 0x1871. The v1.150
	// 0xB5BD body carries one notice byte (755E40).
	ErrCodeCosTarget uint8 = 0x71
	// ErrCodeCosRefused is 49D240's plain 3: every refusal of the pet-cure
	// arm (target missing, dead or not the caller's COS) and of the revival
	// arm (no COS, or the COS is already alive). Only the pet-potion arm
	// uses 0x1871.
	ErrCodeCosRefused     uint8 = 0x03
	ErrCodeItemReuseDelay uint8 = 0x5B
	ErrCodeMultipleCOS    uint8 = 0x5C
	// ErrCodeInputFewerThanRemain is
	// UIIT_MSG_STRGERR_ENABLEINPUT_FEWERTHAN_REMAIN (01:14): a quantity
	// larger than the stack holds. INFERRED from the client notice table;
	// unverified against the official GameServer. Reachability verdict
	// (dump-audited): the retail divide dialog clamps its input to
	// [1, stack-1] via SetNumericRange before composing (sub_59c050
	// @0x0059c22b), so a genuine client never sends an over-remain split -
	// this byte only answers hostile/malformed type-0 packets, where the
	// notice copy still matches the cause.
	ErrCodeInputFewerThanRemain uint8 = 0x14
	// ErrCodeGenderMismatch is UIIT_MSG_STRGERR_GENDER_MISMATCH (01:16,
	// banner + guide): the item's RequiredSex (0 female, 1 male; 2 never
	// refuses) does not match the character (native sub_789c60 bit 0x040,
	// charRecord+0x1ac vs ref[0x6b]).
	ErrCodeGenderMismatch uint8 = 0x16
	// ErrCodePositiveNumberOnly is
	// UIIT_MSG_STRGERR_SPECIFY_POSITIVE_NUMBER_ONLY (01:29): a zero/absent
	// quantity where one is required. INFERRED from the client notice table;
	// unverified against the official GameServer. Reachability verdict
	// (dump-audited): the retail gold dialog clamps to the carried balance
	// and its OK handler silently retires amounts <= 0 without composing
	// (sub_59c510 @0x0059cb0c), and the divide dialog seeds its amount to 1
	// - so zero-quantity/zero-gold requests only arrive from hostile
	// packets, where this byte keeps the semantically matching notice.
	ErrCodePositiveNumberOnly uint8 = 0x29
	// ErrCodeCountryMismatch is UIIT_MSG_STRGERR_COUNTRY_MISMATCH (01:2f,
	// banner + guide): the item's Country (0 China, 1 Europe; 3 never
	// refuses) does not match the character's race (native sub_789c60 bit
	// 0x200, ref[0x27] vs charBody+0x9c).
	ErrCodeCountryMismatch uint8 = 0x2F
	// ErrCodeStrengthRequired / ErrCodeIntellectRequired are
	// UIIT_MSG_STRGERR_HIGHER_STRENGTH_REQUIRED (01:30) /
	// UIIT_MSG_STRGERR_HIGHER_INTELLECT_REQUIRED (01:31), banner + guide:
	// native sub_789c60 bits 0x008/0x010 (STR/INT words player+0x834/+0x836
	// vs ReqStr/ReqInt +0x1b0/+0x1b4). The shipped itemdata carries 0 in
	// every ReqStr/ReqInt cell, so these are unreachable until an item
	// actually requires a stat - and the character stat model lands with
	// them.
	ErrCodeStrengthRequired  uint8 = 0x30
	ErrCodeIntellectRequired uint8 = 0x31
	// ErrCodeExclusiveArmorMix is the clothes/hard body-armor exclusivity
	// refusal (bug C): the client's sub_689420 special body @0x0068960a maps
	// 01:32 to the country-split
	// UIIT_MSG_STRGERR_CANT_MIX_EXCLUSIVE_ARMOR_TYPE strings ("Armor and
	// robe cannot be equipped at the same time."), banner + guide type 5.
	ErrCodeExclusiveArmorMix uint8 = 0x32
	// ErrCodeCantEquipRazed is UIIT_MSG_STRGERR_CANT_EQUIP_RAZED_ITEM
	// (01:37, banner + guide type 5): the razed/broken gate, native
	// sub_789c60 bit 0x004 @0x00789cc7 - an item whose class carries a
	// durability attribute (instance blob (+0xc0)[5]; itemdata Dur_U > 0)
	// refuses to equip while its current durability reads 0.
	ErrCodeCantEquipRazed uint8 = 0x37
	// ErrCodeCannotBePicked is UIIT_MSG_STRGERR_CANNOT_BE_PICKED (01:39):
	// the ground item is already gone.
	ErrCodeCannotBePicked uint8 = 0x39
	// ErrCodeCannotDropEquipped is the equipment-source ground-drop refusal
	// (01:6d). PINNED in v1.150: the drop-confirm dialog sub_68d430
	// @0x0068d430 opens ONLY for an inventory-bag source (kind 0x46,
	// @0x0068d49d); an equipment source (kind 0x47) routes to
	// CGInterface_ShowSystemNotice(gi, 1, 0x6d) @0x0068d606 and abandons
	// without ever composing the type-0x07 packet. So a stock client cannot
	// drop a worn item; a modified client can, and the server must refuse
	// it here (LANE-4, levelup wave; GROK-V4 seq 76, COORD sign-off seq 93).
	// The notice byte routes through sub_75a3d0 -> sub_689420 category 01,
	// where 01:6d resolves to UIIT_MSG_STRGERR_CANT_DROP_EQUIPED_ITEM_DIRECTLY
	// (guide type 5) - the same worn-item message the retail UI shows
	// locally, so the server refusal surfaces the retail notice with no
	// client work.
	ErrCodeCannotDropEquipped uint8 = 0x6D
)

// Object state channels carried by 0x3122's stateType byte (sub_777b60).
const (
	StateChannelLife   uint8 = 0
	StateChannelMove   uint8 = 1
	StateChannelBody   uint8 = 4
	StateChannelPvp    uint8 = 7
	StateChannelBattle uint8 = 8
	StateChannelScroll uint8 = 11
)

// Values for StateChannelLife.
const (
	LifeStateAlive uint8 = 1
	LifeStateDead  uint8 = 2
)

// Values for StateChannelMove. One shared motion enum: the client routes
// the value through sub_858450 / CICharactor_SetRunWalkMode, whose mode
// table handles exactly 0 (stand, motion state 3), 2 (walk, speed channel
// 0), 3 (run, speed channel 1) and 4 (sit, motion state 6); the paired
// SetupActionButton jump table (sub_58bbc0) re-skins the toolbar for the
// same four values.
const (
	MoveStateStand uint8 = 0
	MoveStateWalk  uint8 = 2
	MoveStateRun   uint8 = 3
	MoveStateSit   uint8 = 4
)
