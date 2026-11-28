/*
===========================================================================

characterids.go - every service characterdata row's RefObjID and name by
codename

CharacterRefByCodename carries only the rows buildCharacterRef can read in
full (movement, scale, the summon tail). Static objects - event poles,
flags, keys and doors - have no movement columns and are refused there,
yet a world that places them still needs their RefObjID and display name
on the wire. This index answers for every service row, refused or not.

===========================================================================
*/
package enterworld

import "strings"

// characterNameStrIDColumn is the characterdata NameStrID column (SN_*),
// the key into textdataname.txt.
const characterNameStrIDColumn = 5

/*
================
characterIdentity

One service row's RefObjID and English display name (empty when the row
names no symbol or the symbol has no English text).
================
*/
type characterIdentity struct {
	id   uint32
	name string
}

/*
================
indexCharacterID

Records one characterdata row's id and English name under its codename
when the row is in service (column 0 is 1). Called for every row parse
reads; names is the textdataname.txt symbol -> English map.
================
*/
func (t *TextdataItems) indexCharacterID(fields []string, names map[string]string) {
	if len(fields) <= 2 || strings.TrimSpace(fields[0]) != "1" {
		return
	}
	id, ok := textdataInt(fields[1])
	if !ok || id <= 0 || id > int64(^uint32(0)) {
		return
	}
	identity := characterIdentity{id: uint32(id)}
	if len(fields) > characterNameStrIDColumn {
		identity.name = names[strings.TrimSpace(fields[characterNameStrIDColumn])]
	}
	t.characterIDsByCodename[strings.TrimSpace(fields[2])] = identity
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
	identity, ok := t.characterIDsByCodename[codename]
	return identity.id, ok
}

/*
================
CharacterNameByCodename

The English display name of any service characterdata row, static objects
included (textdataname.txt through the row's NameStrID). False when the
codename is not a service row or its symbol has no English text.
================
*/
func (t *TextdataItems) CharacterNameByCodename(codename string) (string, bool) {
	t.once.Do(t.load)
	identity, ok := t.characterIDsByCodename[codename]
	return identity.name, ok && identity.name != ""
}
