/*
===========================================================================

townarea_test.go - the town parts the thief rule reads (#1985)

A synthetic regioninfo.txt around the Donwhang sectors, whose combat
classes come from the shipped combat table: (152,102) is protected, the
sectors around it are battlefield.

===========================================================================
*/
package townarea

import (
	"strings"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
)

// The fixture: one town with a protected core, an outer part in the
// battlefield region east of it (x 0..600), a line row and a reversed row;
// a "town" with no protected row (a fortress); and a field.
const fixture = "\t\t\t\t\t\t\r\n" +
	"#TOWN\ttown\t\t\t\t\t\r\n" +
	"152\t102\tALL\t\t\t\t\r\n" +
	"154\t102\tRECT\t0\t0\t600\t1920\r\n" +
	"153\t104\tRECT\t0\t640\t1920\t640\r\n" +
	"155\t104\tRECT\t0\t220\t1720\t0\r\n" +
	"\t\t\t\t\t\t\r\n" +
	"#TOWN\tfort\t\t\t\t\t\r\n" +
	"150\t104\tALL\t\t\t\t\r\n" +
	"#FIELD\tfield\t\t\t\t\t\r\n" +
	"156\t104\tALL\t\t\t\t\r\n"

/*
================
region
================
*/
func region(x, z int) uint16 {
	return worldgeom.RegionIDForSectors(x, z)
}

/*
================
requireClasses

The fixture's meaning rests on the shipped combat table.
================
*/
func requireClasses(t *testing.T) {
	t.Helper()
	if allowed, known := worldgeom.RegionPlayerCombat(region(152, 102)); !known || allowed {
		t.Fatal("(152,102) is not a protected region")
	}
	for _, r := range []uint16{region(150, 104), region(153, 104), region(154, 102), region(155, 102)} {
		if allowed, known := worldgeom.RegionPlayerCombat(r); !known || !allowed {
			t.Fatalf("%#x is not a battlefield region", r)
		}
	}
}

/*
================
parseFixture
================
*/
func parseFixture(t *testing.T) *Areas {
	t.Helper()
	requireClasses(t)
	areas, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return areas
}

/*
================
TestParseKeepsTownPartsOutsideProtectedRegions

Only the outer part survives: the protected core is the region rule's,
the line and the reversed row hold no ground, the fortress has no
protected row and the field is no town.
================
*/
func TestParseKeepsTownPartsOutsideProtectedRegions(t *testing.T) {
	areas := parseFixture(t)
	if areas.Len() != 1 {
		t.Fatalf("parts = %d, want 1", areas.Len())
	}
	for _, c := range []struct {
		at   worldgeom.RegionXZ
		want bool
	}{
		{worldgeom.RegionXZ{RegionID: region(154, 102), X: 300, Z: 900}, true},
		// Corners are inclusive, as the client's 414030 compares them.
		{worldgeom.RegionXZ{RegionID: region(154, 102), X: 600, Z: 1920}, true},
		{worldgeom.RegionXZ{RegionID: region(154, 102), X: 601, Z: 900}, false},
		{worldgeom.RegionXZ{RegionID: region(153, 104), X: 900, Z: 640}, false},
		{worldgeom.RegionXZ{RegionID: region(150, 104), X: 900, Z: 900}, false},
		{worldgeom.RegionXZ{RegionID: region(156, 104), X: 900, Z: 900}, false},
		{worldgeom.RegionXZ{RegionID: region(152, 102), X: 900, Z: 900}, false},
	} {
		if got := areas.Contains(c.at); got != c.want {
			t.Errorf("Contains(%#x %v,%v) = %v, want %v", c.at.RegionID, c.at.X, c.at.Z, got, c.want)
		}
	}
}

/*
================
TestEnteredFromBattlefield

A chord from open ground into the outer part enters; one that stops short
of it, one from the protected core into it, one that starts inside it, one that misses it and
one over the line row do not.
================
*/
func TestEnteredFromBattlefield(t *testing.T) {
	areas := parseFixture(t)
	for _, c := range []struct {
		name     string
		from, to worldgeom.RegionXZ
		want     bool
	}{
		{"open ground short of the part",
			worldgeom.RegionXZ{RegionID: region(155, 102), X: 100, Z: 900},
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 1200, Z: 900}, false},
		{"open ground across the part's east edge",
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 900, Z: 900},
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 300, Z: 900}, true},
		{"the protected core into the part",
			worldgeom.RegionXZ{RegionID: region(153, 102), X: 1800, Z: 900},
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 300, Z: 900}, false},
		{"inside the part",
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 100, Z: 900},
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 1500, Z: 900}, false},
		{"over the line row",
			worldgeom.RegionXZ{RegionID: region(153, 104), X: 900, Z: 300},
			worldgeom.RegionXZ{RegionID: region(153, 104), X: 900, Z: 1000}, false},
		{"standing still",
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 900, Z: 900},
			worldgeom.RegionXZ{RegionID: region(154, 102), X: 900, Z: 900}, false},
	} {
		if got := areas.EnteredFromBattlefield(c.from, c.to); got != c.want {
			t.Errorf("%s: entered = %v, want %v", c.name, got, c.want)
		}
	}
	var none *Areas
	if none.EnteredFromBattlefield(
		worldgeom.RegionXZ{RegionID: region(154, 102), X: 900, Z: 900},
		worldgeom.RegionXZ{RegionID: region(154, 102), X: 300, Z: 900},
	) {
		t.Fatal("no town list entered a part")
	}
}

/*
================
TestParseRefusesAMalformedRow
================
*/
func TestParseRefusesAMalformedRow(t *testing.T) {
	for _, row := range []string{"154\t102\tRECT\t0\t0\r\n", "154\t102\tROUND\r\n", "x\t102\tALL\r\n", "300\t102\tALL\r\n"} {
		if _, err := Parse(strings.NewReader("#TOWN\ttown\r\n152\t102\tALL\r\n" + row)); err == nil {
			t.Errorf("row %q parsed", row)
		}
	}
}

/*
================
TestThiefCartTownAreasSwitch

On by default; off is native; anything else stops the boot.
================
*/
func TestThiefCartTownAreasSwitch(t *testing.T) {
	for _, c := range []struct {
		value   string
		on, bad bool
	}{{"", true, false}, {"on", true, false}, {"off", false, false}, {"0", false, false}, {"maybe", false, true}} {
		t.Setenv(EnvThiefCartTownAreas, c.value)
		on, err := ThiefCartTownAreasFromEnv()
		if on != c.on || (err != nil) != c.bad {
			t.Errorf("%q: on=%v err=%v", c.value, on, err)
		}
	}
}
