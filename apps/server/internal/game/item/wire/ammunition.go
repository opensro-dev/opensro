package wire

// OpAvatarInventorySlot7StackCount is the server-owned absolute count for
// the shared shield/quiver equipment socket. The v1.150 receiver
// (client CPSMission_OnActionViewUpdate0x3752) reads exactly one u16,
// writes it into equipment record 7, and applies the ordinary
// empty-record path when the value reaches zero.
const OpAvatarInventorySlot7StackCount uint16 = 0x3752

// AvatarInventorySlot7StackCountFrame synchronizes the post-shot absolute
// ammunition count. This is private actor state and must never be broadcast.
func AvatarInventorySlot7StackCountFrame(count uint16) Frame {
	return Frame{
		Opcode:  OpAvatarInventorySlot7StackCount,
		Payload: NewWriter(2).U16(count).Payload(),
	}
}
