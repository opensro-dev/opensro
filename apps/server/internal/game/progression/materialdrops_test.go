/*
===========================================================================

materialdrops_test.go - the alchemy material drop switch

===========================================================================
*/
package progression

import "testing"

/*
================
TestMaterialDropsSwitch

Only the material family (3/3/11/4) is excluded, and only when on: the
neighbouring tablet, element and rondo families keep their drops.
================
*/
func TestMaterialDropsSwitch(t *testing.T) {
	material := [4]int64{3, 3, 11, 4}
	neighbours := [][4]int64{{3, 3, 11, 3}, {3, 3, 11, 5}, {3, 3, 11, 6}, {3, 3, 10, 1}}
	for _, tc := range []struct {
		env  string
		want bool
	}{{"", false}, {"off", false}, {"0", false}, {"on", true}, {"TRUE", true}, {"1", true}} {
		t.Setenv(EnvAlchemyMaterialDropsOff, tc.env)
		drops := MaterialDropsFromEnv()
		if drops.Off() != tc.want || drops.Excludes(material) != tc.want {
			t.Fatalf("%q: off %v excludes %v, want %v", tc.env, drops.Off(), drops.Excludes(material), tc.want)
		}
		for _, other := range neighbours {
			if drops.Excludes(other) {
				t.Fatalf("%q: %v is not an alchemy material", tc.env, other)
			}
		}
	}
}
