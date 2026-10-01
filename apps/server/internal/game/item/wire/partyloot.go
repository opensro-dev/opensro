/*
===========================================================================

partyloot.go - the party loot notice (0x317D)

CParty_BroadcastLootNotice (server 5BDC80, v1.188 0x3068) tells every party
member what an item-share pickup granted and to whom; the v1.150 client
(CPSMission_OnItemLootGoldOrItem0x317D, 7511C0) prints it in the system chat.

===========================================================================
*/

package wire

// OpPartyLootNotice is the v1.150 client opcode of the loot notice.
const OpPartyLootNotice uint16 = 0x317D

/*
================
EncodePartyLootNotice

[u32 recipient gid][u32 item ref] then the amount in the width 7511C0 reads
for the item's type: a u32 for a gold heap (ETC, group 0x280), a u16 for
another expendable stack, otherwise a single byte.
================
*/
func EncodePartyLootNotice(recipientGid, refObjID uint32, typeFlags uint16, amount uint32) []byte {
	w := NewWriter(12).U32(recipientGid).U32(refObjID)
	expendable := typeFlags&2 == 0 && typeFlags&0x1c == 0xc && typeFlags&0x60 == 0x60
	switch {
	case expendable && typeFlags&0x780 == 0x280:
		w.U32(amount)
	case expendable:
		w.U16(uint16(min(amount, 0xffff)))
	default:
		w.U8(uint8(min(amount, 0xff)))
	}
	return w.Payload()
}
