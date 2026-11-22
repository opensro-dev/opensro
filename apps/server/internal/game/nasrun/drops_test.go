/*
===========================================================================

drops_test.go - tests for drops.go

The odds follow the token families: hard uniques roll the harder
Spirit odds (their ranked variants included), the jewel is the same
everywhere, and a plain monster rolls nothing.

===========================================================================
*/
package nasrun

import "testing"

/*
================
TestLuckyDropOddsFollowTheTokenFamilies
================
*/
func TestLuckyDropOddsFollowTheTokenFamilies(t *testing.T) {
	for _, tc := range []struct {
		codename     string
		spirit, want int
		ok           bool
	}{
		{"MOB_CH_TIGERWOMAN", SpiritDropEasyBps, JewelDropBps, true},
		{"MOB_CH_TIGERWOMAN_L2", SpiritDropEasyBps, JewelDropBps, true},
		{"MOB_EU_KERBEROS_L3", SpiritDropEasyBps, JewelDropBps, true},
		{"MOB_KK_ISYUTARU", SpiritDropHardBps, JewelDropBps, true},
		{"MOB_TK_BONELORD_L2", SpiritDropHardBps, JewelDropBps, true},
		{"MOB_RM_TAHOMET", SpiritDropHardBps, JewelDropBps, true},
		{"MOB_TQ_BLACKSNAKE_L3", SpiritDropHardBps, JewelDropBps, true},
		{"MOB_TQ_WESTGUARDIAN", SpiritDropHardBps, JewelDropBps, true},
		{"MOB_CH_MANGNYANG", 0, 0, false},
		// A longer codename sharing a prefix is not in the family.
		{"MOB_CH_TIGERWOMANX", 0, 0, false},
	} {
		spirit, jewel, ok := LuckyDropOdds(tc.codename)
		if spirit != tc.spirit || jewel != tc.want || ok != tc.ok {
			t.Fatalf("%s odds = %d/%d/%v, want %d/%d/%v", tc.codename, spirit, jewel, ok, tc.spirit, tc.want, tc.ok)
		}
	}
}
