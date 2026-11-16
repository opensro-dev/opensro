/*
===========================================================================

market_test.go - the market flag and the expiry pass

===========================================================================
*/
package market

import (
	"errors"
	"testing"
)

type fakeStore struct {
	limits []int
	err    error
}

func (f *fakeStore) ExpireMarketListings(limit int) (int, error) {
	f.limits = append(f.limits, limit)
	return 3, f.err
}

/*
================
TestFromEnvIsOffByDefault
================
*/
func TestFromEnvIsOffByDefault(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true, "yes": false} {
		t.Setenv(EnvConsignment, value)
		if got := FromEnv(); got != want {
			t.Errorf("%s=%q: FromEnv() = %v, want %v", EnvConsignment, value, got, want)
		}
	}
}

/*
================
TestRulesAreTheOwnersNumbers
================
*/
func TestRulesAreTheOwnersNumbers(t *testing.T) {
	rules := Rules()
	if rules.FeePercent != 1 || rules.DurationMs != 24*60*60*1000 || rules.OpenCap != 20 {
		t.Fatalf("rules = %+v", rules)
	}
	if fee := rules.MarketFee(1000); fee != 10 {
		t.Fatalf("fee on 1000 = %d, want 10", fee)
	}
	if fee := rules.MarketFee(99); fee != 0 {
		t.Fatalf("fee on 99 = %d, want 0", fee)
	}
}

/*
================
TestExpireBoundsEachPass
================
*/
func TestExpireBoundsEachPass(t *testing.T) {
	store := &fakeStore{}
	if returned := Expire(store); returned != 3 || len(store.limits) != 1 || store.limits[0] != ExpiryBatch {
		t.Fatalf("expire = %d, limits %v", returned, store.limits)
	}
	store.err = errors.New("disk")
	Expire(store)
}
