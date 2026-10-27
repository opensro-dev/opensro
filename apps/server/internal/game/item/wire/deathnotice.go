/*
===========================================================================

deathnotice.go - the death notice that sets the death box's rebirth mode

The native server sends the victim one byte, its death kind
(victim+0x1CB4), as opcode 0x3011. The v1.150
client reads it as 0x37CE (CPSMission_OnDeathNoticeSetRebirthMode 74FE50)
and stores it at CGInterface+0x640 (67B2E0); the death box labels its first
button from that byte (CIFMessageBox_ConfigureByType 52FF33).

===========================================================================
*/

package wire

// OpDeathNotice is the v1.150 client's opcode for server 0x3011.
const OpDeathNotice uint16 = 0x37CE

/*
================
DeathNoticeFrame

[u8 death kind], the victim's private notice.
================
*/
func DeathNoticeFrame(kind uint8) Frame {
	return Frame{Opcode: OpDeathNotice, Payload: []byte{kind}}
}
