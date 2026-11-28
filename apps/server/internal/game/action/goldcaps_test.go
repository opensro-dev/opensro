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
	rt.StorageGoldDepositLimit = 2_000_000_000
	if rt.storageGoldDepositLimit() != 2_000_000_000 {
		t.Fatalf("configured: deposit %d, want 2000000000", rt.storageGoldDepositLimit())
	}
}

/*
================
TestStorageGoldDepositLimitFromEnv

The switch's raised value, a shard's bare number clamped to the u32 a
gold move carries, and the native cap for everything else.
================
*/
func TestStorageGoldDepositLimitFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  uint64
	}{
		{"", NativeStorageGoldDepositLimit},
		{"off", NativeStorageGoldDepositLimit},
		{"junk", NativeStorageGoldDepositLimit},
		{"0", NativeStorageGoldDepositLimit},
		// The switch only raises: a smaller number keeps the native cap.
		{"50", NativeStorageGoldDepositLimit},
		{"99999999", NativeStorageGoldDepositLimit},
		{"100000001", 100_000_001},
		{"on", RaisedStorageGoldDepositLimit},
		{"1", RaisedStorageGoldDepositLimit},
		{"true", RaisedStorageGoldDepositLimit},
		{"500000000", 500_000_000},
		{"4294967295", 4_294_967_295},
		{"10000000000", 10_000_000_000},
		{"9007199254740992", MaxStorageGoldDepositLimit},
		{"99999999999999999999", MaxStorageGoldDepositLimit},
	} {
		t.Setenv(EnvRaisedGoldCaps, tc.value)
		if got := StorageGoldDepositLimitFromEnv(); got != tc.want {
			t.Fatalf("%s=%q: %d, want %d", EnvRaisedGoldCaps, tc.value, got, tc.want)
		}
	}
}
