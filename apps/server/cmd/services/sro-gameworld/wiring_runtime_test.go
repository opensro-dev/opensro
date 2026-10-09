package main

import (
	"testing"
	"time"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

func TestEnterWorldVerifierRejectsForeignShardBeforeTokenVerification(
	t *testing.T,
) {
	const ownedShard = "global-official"
	const foreignShard = "test"
	verified := false
	gate := enterWorldVerifierForShard(
		ownedShard,
		func(token, division, character string) (int, error) {
			verified = true
			return 6, nil
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
