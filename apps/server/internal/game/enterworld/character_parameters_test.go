/*
===========================================================================

character_parameters_test.go - complete COS type and RefObjChar projection

Keep attack/transport/pet bands and elemental column order visible at the
loader boundary; synthetic runtime references cannot catch missing columns.

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"
)

/*
================
TestCharacterReferencePreservesCosBandAndKeeperColumns
================
*/
func TestCharacterReferencePreservesCosBandAndKeeperColumns(t *testing.T) {
	for band := 1; band <= 4; band++ {
		fields := make([]string, 120)
		for index := range fields {
			fields[index] = "0"
		}
		fields[113] = "5"
		if band <= 2 {
			fields[72] = "1"
		}
		fields[0], fields[1], fields[2] = "1", "9", "COS_TEST"
		fields[8], fields[9], fields[10], fields[11], fields[12] = "1", "1", "2", "3", strconv.Itoa(band)
		fields[46], fields[47], fields[48], fields[50] = "20", "80", "100", "10"
		fields[57], fields[59], fields[60], fields[80] = "1", "1000", "600", "25"
		for index := range 6 {
			fields[61+index] = strconv.Itoa((index + 1) * 10)
		}
		ref := buildCharacterRef(fields, nil)
		if ref == nil || ref.TidWord != uint16(band<<11|0x1c6) {
			t.Fatalf("band %d: %+v", band, ref)
		}
		if ref.CanRide != (band <= 2) {
			t.Fatalf("band %d lost authored ride permission", band)
		}
		if band == 3 && ref.SatietyMinutes != 5 || band != 3 && ref.SatietyMinutes != 0 {
			t.Fatalf("band %d hunger projection: %d", band, ref.SatietyMinutes)
		}
		if !ref.Parameters.CombatPinned || ref.Parameters.MagicalParry != 25 || ref.Parameters.BodyRadius != 10 ||
			ref.Parameters.ElementResist != [6]uint8{10, 20, 40, 30, 50, 60} {
			t.Fatalf("band %d keeper projection: %+v", band, ref.Parameters)
		}
	}
}
