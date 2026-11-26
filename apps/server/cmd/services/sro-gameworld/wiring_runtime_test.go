/*
===========================================================================

wiring_runtime_test.go - the runtime's composition: shard gates, transport
defaults, and the timing sinks a plane without a hub must leave unset

===========================================================================
*/
package main

import (
	"testing"
	"time"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestEnterWorldVerifierRejectsForeignShardBeforeTokenVerification
================
*/
func TestEnterWorldVerifierRejectsForeignShardBeforeTokenVerification(
	t *testing.T,
) {
	const ownedShard = "global-official"
	const foreignShard = "test"
	verified := false
	gate := enterWorldVerifierForShard(
		ownedShard,
		func(token, division, character string) error {
			verified = true
			return nil
		},
	)
	ok, code := gate(nil, transport.EnterWorld{
		Division:  foreignShard,
		CharName:  "ForeignHero",
		AuthToken: []byte("otherwise-valid"),
	})
	if ok || code != auth.DenyCodeUnauthorized {
		t.Fatalf("foreign shard = (%v, %#x), want unauthorized", ok, code)
	}
	if verified {
		t.Fatal("foreign shard reached token verifier")
	}
}

/*
================
TestEnterWorldVerifierAcceptsOwnedShardToken
================
*/
func TestEnterWorldVerifierAcceptsOwnedShardToken(t *testing.T) {
	const secret = "enterworld-test-secret-at-least-32-bytes"
	const shardID = "global-official"
	const character = "OwnedHero"
	now := time.Unix(1_785_000_000, 0)
	token, err := auth.Mint(
		[]byte(secret),
		shardID,
		character,
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	gate := enterWorldVerifierForShard(
		shardID,
		auth.Verifier([]byte(secret), func() time.Time { return now }),
	)
	ok, code := gate(nil, transport.EnterWorld{
		Division:  shardID,
		CharName:  character,
		AuthToken: []byte(token),
	})
	if !ok || code != 0 {
		t.Fatalf("owned shard = (%v, %#x), want accepted", ok, code)
	}
}

/*
================
TestTransportDefaultsComeFromOwnedShard
================
*/
func TestTransportDefaultsComeFromOwnedShard(t *testing.T) {
	definition := shard.Definition{
		ID:           "test",
		TransportURL: "https://127.0.0.1:8793",
	}
	config := transport.DefaultConfig()
	if err := applyOwnedShardTransportDefaults(
		definition,
		&config,
		false,
		false,
	); err != nil {
		t.Fatal(err)
	}
	if config.WTAddr != "127.0.0.1:8793" ||
		config.WSAddr != "127.0.0.1:8793" {
		t.Fatalf(
			"transport = wt %q ws %q, want test shard :8793",
			config.WTAddr,
			config.WSAddr,
		)
	}

	config.WTAddr = "0.0.0.0:443"
	config.WSAddr = "0.0.0.0:8443"
	if err := applyOwnedShardTransportDefaults(
		definition,
		&config,
		true,
		true,
	); err != nil {
		t.Fatal(err)
	}
	if config.WTAddr != "0.0.0.0:443" ||
		config.WSAddr != "0.0.0.0:8443" {
		t.Fatalf("explicit deployment binds were overwritten: %+v", config)
	}
}

/*
================
TestMissionTickerWithoutAHubDropsSlowReports

A plane with no transport hub (the wiring tests' fixture) must leave the
slow-step and publication-hold sinks unset. Bound to the nil hub they
panicked the first time a hold crossed SlowHookThreshold, which only a
loaded machine reached in the beta-silk barrier test; here the hold is
made long on purpose.
================
*/
func TestMissionTickerWithoutAHubDropsSlowReports(t *testing.T) {
	game, _, _ := betaWiringFixture()
	game.newMissionTicker(&action.PeerReferenceCatalog{})
	unlock := game.items.LockPublication(betaWiringDivision, "test")
	// The runtime times the hold with the wall clock (publicationRelease),
	// so the window is spent, not simulated.
	wait.Consistently(t, simulation.SlowHookThreshold+10*time.Millisecond, "the publication gate stays held", func() bool { return true })
	unlock()
	// The runtime calls the slow-step sink only when it is set, as here.
	if slow := game.items.Steps.Slow; slow != nil {
		slow("test", simulation.SlowHookThreshold)
	}
}
