/*
===========================================================================

skillobject.go - native CISkillObj object-list and single-spawn rows

Dynamic skill objects carry a type discriminator instead of a catalog
reference. Their skill ID selects the client model; their GID owns lifetime.

===========================================================================
*/
package wire

const DynamicObjectReference uint32 = 0xffffffff
const SkillObjectType uint16 = 0x54
const skillObjectRowBytes = 30

/*
================
SkillObjectSpawn

48CE60 writes the skill followed by 484FB0's object header. Client 86C440
reads the same row; 86C420 consumes the single-spawn appearance tail.
================
*/
type SkillObjectSpawn struct {
	SkillID uint32
	GID     uint32
	Region  uint16
	X, Y, Z float32
	Heading uint16
	Appear  uint8
}

/*
================
Encode

Object-list rows omit the appearance byte. Keeping both forms here prevents
bootstrap and live publication from disagreeing on the dynamic envelope.
================
*/
func (row SkillObjectSpawn) Encode(single bool) []byte {
	w := NewWriter(skillObjectRowBytes + 1)
	w.U32(DynamicObjectReference).U16(SkillObjectType).U32(row.SkillID)
	w.U32(row.GID).U16(row.Region).F32(row.X).F32(row.Y).F32(row.Z).U16(row.Heading)
	if single {
		w.U8(row.Appear)
	}
	return w.Payload()
}
