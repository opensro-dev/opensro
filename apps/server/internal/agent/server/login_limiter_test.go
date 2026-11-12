/*
===========================================================================

login_limiter_test.go - the Agent's one key spelling in front of the pacer

===========================================================================
*/

package agentserver

import (
	"testing"
	"time"
)

/*
================
TestLoginLimiterSharesOneBucketPerHost

Two ports of one host spend one allowance, as the limiter did before
the bucket moved to platform/pacer; another host keeps its own.
================
*/
func TestLoginLimiterSharesOneBucketPerHost(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	limiter := newLoginLimiter(func() time.Time { return now })
	for attempt := 0; attempt < loginAttemptBurst; attempt++ {
		port := "203.0.113.7:4000"
		if attempt%2 == 1 {
			port = "203.0.113.7:4001"
		}
		if !limiter.Allow(port) {
			t.Fatalf("attempt %d refused inside the burst", attempt)
		}
	}
	if limiter.Allow("203.0.113.7:4999") {
		t.Fatal("a third port of the same host got a fresh allowance")
	}
	if !limiter.Allow("198.51.100.2:4000") {
		t.Fatal("another host was refused")
	}
	if !limiter.Allow("") || !limiter.Allow("unknown") {
		t.Fatal("the unknown key is one bucket inside its burst")
	}
}
