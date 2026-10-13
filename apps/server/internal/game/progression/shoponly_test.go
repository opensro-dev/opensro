/*
===========================================================================

shoponly_test.go - tests for shoponly.go

===========================================================================
*/

package progression

import "testing"

/*
================
TestShopOnlyDegree9
================
*/
func TestShopOnlyDegree9(t *testing.T) {
	luckyPowder, magicStone, attrStone, elixir := [4]int64{3, 3, 10, 2}, [4]int64{3, 3, 11, 1}, [4]int64{3, 3, 11, 2}, [4]int64{3, 3, 10, 1}
	specialStone, tablet := [4]int64{3, 3, 11, 7}, [4]int64{3, 3, 11, 3}
	for _, value := range []string{"", "off", "yes"} {
		t.Setenv(EnvBetaShopOnlyDegree9, value)
		if rule := BetaShopOnlyDegree9FromEnv(); rule.Enabled() || rule.Excludes(magicStone, 9) {
			t.Fatalf("%q excluded a drop", value)
		}
	}
	t.Setenv(EnvBetaShopOnlyDegree9, "on")
	rule := BetaShopOnlyDegree9FromEnv()
	for _, tc := range []struct {
		typeIDs [4]int64
		class   int
		want    bool
	}{
		{luckyPowder, 9, true}, {magicStone, 9, true}, {attrStone, 9, true},
		{luckyPowder, 8, false}, {magicStone, 10, false}, {attrStone, 1, false},
		{specialStone, 9, true}, {specialStone, 8, false}, {tablet, 9, false},
		{elixir, 2, false}, {elixir, 9, false}, {[4]int64{3, 1, 6, 1}, 9, false},
	} {
		if rule.Excludes(tc.typeIDs, tc.class) != tc.want {
			t.Fatalf("%v class %d: excluded=%v", tc.typeIDs, tc.class, !tc.want)
		}
	}
}
