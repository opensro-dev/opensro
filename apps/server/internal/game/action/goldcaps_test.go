/*
===========================================================================

goldcaps_test.go - the raised stall and warehouse gold caps

===========================================================================
*/
package action

import "testing"

/*
================
TestRaisedGoldCapsFromEnv

Only "on", "1" and "true" turn the wider caps on; unset and anything else
keep the native caps.
================
*/
func TestRaisedGoldCapsFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false}, {"off", false}, {"0", false}, {"on", true}, {"1", true}, {"true", true}, {"TRUE", true},
	} {
		t.Setenv(EnvRaisedGoldCaps, tc.value)
		if got := RaisedGoldCapsFromEnv(); got != tc.want {
			t.Fatalf("%s=%q: %v, want %v", EnvRaisedGoldCaps, tc.value, got, tc.want)
		}
	}
}

/*
================
TestGoldCapsFollowTheSwitch

The runtime's two limits stay native with the switch off and take the
raised values with it on.
================
*/
func TestGoldCapsFollowTheSwitch(t *testing.T) {
	rt := &Runtime{}
	if rt.stallPriceLimit() != NativeStallPriceLimit || rt.storageGoldDepositLimit() != NativeStorageGoldDepositLimit {
		t.Fatalf("off: stall %d deposit %d, want %d/%d", rt.stallPriceLimit(), rt.storageGoldDepositLimit(),
			NativeStallPriceLimit, NativeStorageGoldDepositLimit)
	}
	rt.RaisedGoldCaps = true
	if rt.stallPriceLimit() != RaisedStallPriceLimit || rt.storageGoldDepositLimit() != RaisedStorageGoldDepositLimit {
		t.Fatalf("on: stall %d deposit %d, want %d/%d", rt.stallPriceLimit(), rt.storageGoldDepositLimit(),
			RaisedStallPriceLimit, RaisedStorageGoldDepositLimit)
	}
}
