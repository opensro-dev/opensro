/*
===========================================================================

dungeon_admission_test.go - which dungeon regions the shard runs

Port-only, not native: the Pharaoh Temple's seven dungeons (0x800A-0x8010)
are admitted only while SRO_PHARAOH_TEMPLE is on; every native dungeon is
admitted either way.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/pharaohtemple"
)

/*
================
TestTempleDungeonsAreRefusedWhileTheSwitchIsOff

Off (unset, "off", and a value the switch refuses) admits exactly the
native regions: the Donwhang cave, the Qin-Shi floors, the flag-war arena.
On admits the temple's seven as well, and nothing next to them.
================
*/
func TestTempleDungeonsAreRefusedWhileTheSwitchIsOff(t *testing.T) {
	native := []uint16{0x8001, 0x8002, 0x8007, 0x8009, 0x8011, 0x8000}
	for _, value := range []string{"", "off", "0", "false", "nonsense"} {
		t.Setenv(pharaohtemple.EnvPharaohTemple, value)
		for _, region := range native {
			if !dungeonRegionAdmitted(region) {
				t.Fatalf("switch %q refused native region 0x%04X", value, region)
			}
		}
		for _, dungeon := range pharaohtemple.Dungeons() {
			if dungeonRegionAdmitted(dungeon.Region) {
				t.Fatalf("switch %q admitted temple region 0x%04X (%s)", value, dungeon.Region, dungeon.Name)
			}
		}
	}
	for _, value := range []string{"1", "on", "true", "ON"} {
		t.Setenv(pharaohtemple.EnvPharaohTemple, value)
		for _, region := range native {
			if !dungeonRegionAdmitted(region) {
				t.Fatalf("switch %q refused native region 0x%04X", value, region)
			}
		}
		for _, dungeon := range pharaohtemple.Dungeons() {
			if !dungeonRegionAdmitted(dungeon.Region) {
				t.Fatalf("switch %q refused temple region 0x%04X (%s)", value, dungeon.Region, dungeon.Name)
			}
		}
	}
}
