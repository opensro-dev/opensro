/*
===========================================================================

spawnskills.go - the active effect list on a non-local spawn row

CICharactor_DeserializeSpawnData (85FB20) reads, after the scale, a u8
count of effects. For each: the skill id; its instance token when the
skill's extended info has a target type (CSkillData_GetExtendedInfo); and a
status byte when the skill record's +0x274 flag is set (1 marks the
creator). A non-local actor carries no remaining-duration word. The shape
of each entry is therefore a property of its skill, not of the row.

===========================================================================
*/

package wire

import "fmt"

// maxSpawnSkills is the u8 count's capacity.
const maxSpawnSkills = 255

/*
================
SpawnSkillEntry

One active effect on a spawn row. Token rides when HasToken, Status when
HasStatus; both come from the effect's skill record.
================
*/
type SpawnSkillEntry struct {
	SkillID   uint32
	Token     uint32
	HasToken  bool
	Status    uint8
	HasStatus bool
}

/*
================
SpawnSkillShape

Which optional fields a skill's entry carries, for decoding.
================
*/
type SpawnSkillShape func(skillID uint32) (hasToken, hasStatus bool)

/*
================
writeSpawnSkills
================
*/
func writeSpawnSkills(w *Writer, entries []SpawnSkillEntry) {
	if len(entries) > maxSpawnSkills {
		entries = entries[:maxSpawnSkills]
	}
	w.U8(uint8(len(entries)))
	for _, entry := range entries {
		w.U32(entry.SkillID)
		if entry.HasToken {
			w.U32(entry.Token)
		}
		if entry.HasStatus {
			w.U8(entry.Status)
		}
	}
}

/*
================
readSpawnSkills
================
*/
func readSpawnSkills(r *Reader, shape SpawnSkillShape) ([]SpawnSkillEntry, error) {
	count, err := r.U8()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	if shape == nil {
		return nil, fmt.Errorf("wire: spawn row carries %d effects but no skill shape was given", count)
	}
	entries := make([]SpawnSkillEntry, 0, count)
	for i := 0; i < int(count); i++ {
		entry := SpawnSkillEntry{}
		if entry.SkillID, err = r.U32(); err != nil {
			return nil, err
		}
		entry.HasToken, entry.HasStatus = shape(entry.SkillID)
		if entry.HasToken {
			if entry.Token, err = r.U32(); err != nil {
				return nil, err
			}
		}
		if entry.HasStatus {
			if entry.Status, err = r.U8(); err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
