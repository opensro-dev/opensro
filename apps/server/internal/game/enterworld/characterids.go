/*
===========================================================================

characterids.go - every service characterdata row's RefObjID by codename

CharacterRefByCodename carries only the rows buildCharacterRef can read in
full (movement, scale, the summon tail). Static objects - event poles,
flags, keys and doors - have no movement columns and are refused there,
yet a world that places them still needs their RefObjID on the wire. This
index answers for every service row, refused or not.

===========================================================================
*/
package enterworld

import "strings"

/*
================
indexCharacterID

Records one characterdata row's id under its codename when the row is in
service (column 0 is 1). Called for every row parse reads.
================
*/
func (t *TextdataItems) indexCharacterID(fields []string) {
	if len(fields) <= 2 || strings.TrimSpace(fields[0]) != "1" {
		return
	}
	if id, ok := textdataInt(fields[1]); ok && id > 0 && id <= int64(^uint32(0)) {
		t.characterIDsByCodename[strings.TrimSpace(fields[2])] = uint32(id)
	}
}

/*
================
CharacterRefObjIDByCodename

The RefObjID of any service characterdata row, static objects included:
the rows CharacterRefByCodename carries plus those it refuses.
================
*/
func (t *TextdataItems) CharacterRefObjIDByCodename(codename string) (uint32, bool) {
	t.once.Do(t.load)
	id, ok := t.characterIDsByCodename[codename]
	return id, ok
}
