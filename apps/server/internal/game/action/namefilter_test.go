/*
===========================================================================

namefilter_test.go - the job alias passes the native name filter

===========================================================================
*/

package action

import (
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/namefilter"
)

/*
================
testNameFilter

Letters, digits and underscore allowed; "rude" refused anywhere.
================
*/
func testNameFilter(t *testing.T) *namefilter.Filter {
	t.Helper()
	text := "#ALLOW_ID_TABLE\t0x0030\t0x0039\r\n#ALLOW_ID_TABLE\t0x0041\t0x005A\r\n" +
		"#ALLOW_ID_TABLE\t0x005F\t0x005F\r\n#ALLOW_ID_TABLE\t0x0061\t0x007A\r\nrude\t2\t2\t2\t2\t"
	data := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(text)) {
		data = append(data, byte(u), byte(u>>8))
	}
	f, err := namefilter.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

/*
================
TestJobAliasPassesTheNameFilter

Natively an alias the filter refuses answers the alias-rule error (0x23)
in either mode and is never taken.
================
*/
func TestJobAliasPassesTheNameFilter(t *testing.T) {
	rt, _, c := jobFixture(t)
	rt.NameFilter = testNameFilter(t)
	level := int64(20)
	c.Level = &level
	rt.HandleJobJoin(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(domain.JobTrader).Payload())
	for _, mode := range []uint8{jobAliasModeCheck, jobAliasModeCreate} {
		out := rt.HandleJobAlias(testDivision, c, wire.NewWriter(13).U32(jobTestNpc).U8(mode).Str("SoRudeGuy").Payload())
		if out.Frames[0].Payload[0] != 2 || out.Frames[0].Payload[1] != jobErrAliasRule || c.Job.Alias != "" {
			t.Fatalf("mode %d: filtered alias answered %x, alias %q", mode, out.Frames[0].Payload, c.Job.Alias)
		}
	}
	rt.HandleJobAlias(testDivision, c, wire.NewWriter(12).U32(jobTestNpc).U8(jobAliasModeCreate).Str("Merchant").Payload())
	if c.Job.Alias != "Merchant" {
		t.Fatalf("a clean alias was refused: %q", c.Job.Alias)
	}
}
