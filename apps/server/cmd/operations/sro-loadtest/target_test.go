/*
===========================================================================

target_test.go - the target guard and the per-bot source addresses

The guard is the difference between a load tool and an accident: the
live service must be refused under every flag combination, and a
staging host must need its explicit opt-in.
===========================================================================
*/

package main

import "testing"

/*
================
TestCheckTargetURL
================
*/
func TestCheckTargetURL(t *testing.T) {
	allowed := []string{
		"",
		"http://127.0.0.1:8787",
		"http://localhost:8788",
		"http://[::1]:8788",
		"https://127.0.0.2:8789",
	}
	for _, raw := range allowed {
		if err := checkTargetURL(raw, false); err != nil {
			t.Fatalf("checkTargetURL(%q) = %v, want allowed", raw, err)
		}
	}
	// The live service is refused with and without the remote opt-in,
	// under the spellings DNS treats as the same name (case, root dot).
	for _, raw := range []string{
		"http://opensro.online",
		"https://play.opensro.online:8787",
		"http://agent.opensro.online",
		"http://OPENSRO.ONLINE:8787",
		"http://Opensro.Online:8787",
		"http://api.OPENSRO.online:8787",
		"http://opensro.online.:8787",
	} {
		for _, allowRemote := range []bool{false, true} {
			if err := checkTargetURL(raw, allowRemote); err == nil {
				t.Fatalf("checkTargetURL(%q, allowRemote=%v) succeeded, want refusal", raw, allowRemote)
			}
		}
	}
	// A non-loopback staging host needs the explicit opt-in.
	if err := checkTargetURL("http://192.0.2.10:8787", false); err == nil {
		t.Fatal("staging host without -allow-remote succeeded, want refusal")
	}
	if err := checkTargetURL("http://192.0.2.10:8787", true); err != nil {
		t.Fatalf("staging host with -allow-remote = %v, want allowed", err)
	}
	for _, raw := range []string{"://missing-scheme", "ftp://127.0.0.1:21", "http://"} {
		if err := checkTargetURL(raw, true); err == nil {
			t.Fatalf("checkTargetURL(%q) succeeded, want refusal", raw)
		}
	}
}

/*
================
TestBotSourceIP

One distinct loopback address per bot, so the Agent's per-address
login limiter sees the fleet the way the edge would present it.
================
*/
func TestBotSourceIP(t *testing.T) {
	if got := botSourceIP(0).String(); got != "127.1.0.0" {
		t.Fatalf("botSourceIP(0) = %s, want 127.1.0.0", got)
	}
	if got := botSourceIP(500).String(); got != "127.1.1.244" {
		t.Fatalf("botSourceIP(500) = %s, want 127.1.1.244", got)
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		ip := botSourceIP(i)
		if !ip.IsLoopback() {
			t.Fatalf("botSourceIP(%d) = %s, not loopback", i, ip)
		}
		if seen[ip.String()] {
			t.Fatalf("botSourceIP(%d) = %s, already used", i, ip)
		}
		seen[ip.String()] = true
	}
}
