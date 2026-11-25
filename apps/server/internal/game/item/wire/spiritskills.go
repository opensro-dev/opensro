/*
===========================================================================

spiritskills.go - the skills a worn Devil's Spirit grants, for its owner

Port-only, not native to v1.150 (SRO_NASRUN, #4384): the later clients'
equipment window shows a worn Spirit's skills in a strip under the
avatar sockets (resinfo/ifequipskillslotwnd.txt, CIFEquipSkillSlotWnd),
and its wearer casts them although they are not learned. The server owns
which skills those are (nasrun.SpiritAbility, action/spiritability.go);
this frame tells the owner whenever the set changes: the Spirit worn,
taken off or lapsed. Entry carries the same list in the bootstrap
(enterworld spiritSkills). 0x3FC9 is a port-only opcode: no v1.150
client or v1.188 server uses it.

Body: u8 count, then count x u32 skill id (empty: no Spirit's skills).

===========================================================================
*/
package wire

// OpSpiritSkills is the port-only S->C Spirit skill list.
const OpSpiritSkills uint16 = 0x3FC9

// MaxSpiritSkills bounds the list: a Spirit grants at most two skills;
// the strip has four slots.
const MaxSpiritSkills = 4

/*
================
SpiritSkillsFrame
================
*/
func SpiritSkillsFrame(skills []uint32) Frame {
	if len(skills) > MaxSpiritSkills {
		skills = skills[:MaxSpiritSkills]
	}
	w := NewWriter(1 + 4*len(skills)).U8(uint8(len(skills)))
	for _, id := range skills {
		w.U32(id)
	}
	return Frame{Opcode: OpSpiritSkills, Payload: w.Payload()}
}
