package enterworld

import "strings"

// PlayerRefObjTIDWord is the native CICUser classification word:
// character bit | TID1 character | TID2 player.
const PlayerRefObjTIDWord uint16 = 0x0026

// CharacterModelRefObjSnapshot returns the complete playable model catalogue
// for the browser's once-per-session RefObjData mirror. Peer visibility is a
// streaming lane: a character model first seen on a later 0x30D7 cannot be
// admitted unless its RefObj row was present before packet dispatch began.
func CharacterModelRefObjSnapshot(roster *Roster) []RefObjRow {
	if roster == nil {
		return []RefObjRow{}
	}
	rows := make([]RefObjRow, 0, len(roster.Models))
	seen := make(map[uint32]bool, len(roster.Models))
	for _, model := range roster.Models {
		if model.RefObjID == 0 || model.Codename == "" || seen[model.RefObjID] {
			continue
		}
		seen[model.RefObjID] = true
		country, sex := nativeModelSelectors(model.Codename)
		rows = append(rows, RefObjRow{
			RefObjID:       model.RefObjID,
			TidWord:        PlayerRefObjTIDWord,
			Codename:       model.Codename,
			Kind:           "player",
			BodyRadius:     model.BodyRadius,
			CountryByte9C:  bytePointer(country),
			SexSelector1AC: bytePointer(sex),
		})
	}
	return rows
}

func nativeModelSelectors(codename string) (country, sex uint8) {
	upper := strings.ToUpper(codename)
	if strings.Contains(upper, "_EU_") {
		country = 1
	}
	// Native selector is inverse to the API enum: 0 female, 1 male.
	if !strings.Contains(upper, "_WOMAN_") {
		sex = 1
	}
	return country, sex
}

func bytePointer(value uint8) *uint8 {
	return &value
}
