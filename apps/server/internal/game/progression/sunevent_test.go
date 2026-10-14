/*
===========================================================================

sunevent_test.go - tests for sunevent.go

===========================================================================
*/

package progression

import "testing"

/*
================
TestSunEventOnly

Only sealed equipment of ItemClass 27 (degree-9 Sun) is excluded, and only
with the flag on: not the ordinary degree-9 item of the same class, not
degree-9 Star or Moon, not degree-8 Sun.
================
*/
func TestSunEventOnly(t *testing.T) {
	blade := [4]int64{3, 1, 6, 1}
	for _, value := range []string{"", "off", "yes"} {
		t.Setenv(EnvSunEventOnly, value)
		if rule := SunEventOnlyFromEnv(); rule.Enabled() || rule.Excludes(blade, itemRaritySealed, 27) {
			t.Fatalf("%q excluded Sun", value)
		}
	}
	t.Setenv(EnvSunEventOnly, "on")
	rule := SunEventOnlyFromEnv()
	for _, tc := range []struct {
		typeIDs       [4]int64
		rarity, class int
		want          bool
	}{
		{blade, 2, 27, true},
		{[4]int64{3, 1, 1, 3}, 2, 27, true}, // armour
		{blade, 0, 27, false},               // the ordinary degree-9 item
		{blade, 2, 26, false},               // degree-9 Moon
		{blade, 2, 25, false},               // degree-9 Star
		{blade, 2, 24, false},               // degree-8 Sun
		{[4]int64{3, 3, 11, 1}, 2, 27, false},
	} {
		if rule.Excludes(tc.typeIDs, tc.rarity, tc.class) != tc.want {
			t.Fatalf("%v rarity %d class %d: excluded=%v", tc.typeIDs, tc.rarity, tc.class, !tc.want)
		}
	}
}
