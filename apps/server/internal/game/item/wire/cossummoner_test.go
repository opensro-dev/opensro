/*
===========================================================================

cossummoner_test.go - state, lease and framing tests for retained pet items

===========================================================================
*/
package wire

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestPersistentSummonerWireStatesAndFraming
================
*/
func TestPersistentSummonerWireStatesAndFraming(t *testing.T) {
	for _, subtype := range []uint16{1, 2} {
		flags := uint16(0xcc) | subtype<<11
		for _, state := range []uint8{1, 2, 3, 4} {
			var pet *domain.CharacterCOS
			if state != 1 {
				pet = &domain.CharacterCOS{RefObjID: 345, Name: "雪 Wolf", Summoned: state == 2, RentalRemainingSeconds: 123, Rentals: []domain.COSRental{{Kind: 0, ID: 42, RemainingSeconds: 60}, {Kind: 5, ID: 90, RemainingSeconds: 70, Tag: 9, Flag: 1}}}
				if state != 4 {
					pet.StateFlags = 1
				}
			}
			body, err := encodeCosSummoner(flags, pet)
			if err != nil {
				t.Fatal(err)
			}
			reader := NewReader(append(append([]byte(nil), body...), 0x57, 0x24))
			decoded, err := readCosSummoner(reader, flags)
			if err != nil || domain.COSItemState(decoded) != state || reader.Remaining() != 2 {
				t.Fatalf("state %d framing: %+v %v", state, decoded, err)
			}
			if pet != nil && (decoded.RefObjID != pet.RefObjID || decoded.Name != pet.Name || !reflect.DeepEqual(decoded.Rentals, pet.Rentals)) {
				t.Fatal("lost retained item fields")
			}
			if subtype == 2 && pet != nil && decoded.RentalRemainingSeconds != 123 {
				t.Fatal("pickup lease lost")
			}
			for end := 0; end < len(body); end++ {
				if _, err := readCosSummoner(NewReader(body[:end]), flags); err == nil {
					t.Fatalf("accepted truncation %d/%d", end, len(body))
				}
			}
		}
	}
	for _, state := range []uint8{0, 5, 255} {
		if _, err := readCosSummoner(NewReader([]byte{state}), 0x8cc); err == nil {
			t.Fatal("accepted invalid state", state)
		}
	}
}
