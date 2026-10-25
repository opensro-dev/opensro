package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTransportAdmissionBindsAccountShardAndExpiry(t *testing.T) {
	secret := []byte("transport-admission-test-secret-32-bytes")
	now := time.Unix(1_800_000_000, 0)
	token, err := MintTransportAdmission(secret, "alice", "global-official", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := VerifyTransportAdmission(secret, token, now)
	if err != nil {
		t.Fatal(err)
	}
	if claims.AccountID != "alice" || claims.ShardID != "global-official" {
		t.Fatalf("claims = %+v", claims)
	}

	parts := strings.Split(token, ".")
	parts[3] = "dGVzdA" // canonical base64url for a different shard.
	if _, err := VerifyTransportAdmission(secret, strings.Join(parts, "."), now); !errors.Is(err, ErrTransportAdmissionForged) {
		t.Fatalf("tampered shard err = %v, want forged", err)
	}
	if _, err := VerifyTransportAdmission(secret, token, now.Add(2*time.Minute)); !errors.Is(err, ErrTransportAdmissionExpired) {
		t.Fatalf("expired err = %v, want expired", err)
	}
}

func TestTransportAdmissionVerifierConsumesOnce(t *testing.T) {
	secret := []byte("transport-admission-test-secret-32-bytes")
	now := time.Unix(1_800_000_000, 0)
	token, err := MintTransportAdmission(secret, "alice", "global-official", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	verify := TransportAdmissionVerifier(secret, func() time.Time { return now })
	if _, err := verify(token); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if _, err := verify(token); !errors.Is(err, ErrTransportAdmissionReplay) {
		t.Fatalf("second verify = %v, want replay", err)
	}
}

func TestTransportAdmissionLedgerDefersConsumption(t *testing.T) {
	secret := []byte("transport-admission-test-secret-32-bytes")
	now := time.Unix(1_800_000_000, 0)
	token, err := MintTransportAdmission(secret, "alice", "global-official", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewTransportAdmissionLedger(secret, func() time.Time { return now })
	// Authentication alone never consumes: a connection that dies
	// before WELCOME leaves the ticket presentable on the fallback.
	for attempt := 0; attempt < 2; attempt++ {
		claims, err := ledger.Verify(token)
		if err != nil {
			t.Fatalf("verify %d: %v", attempt, err)
		}
		if claims.AccountID != "alice" || claims.ShardID != "global-official" {
			t.Fatalf("claims = %+v", claims)
		}
	}
	if err := ledger.Consume(token); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := ledger.Consume(token); !errors.Is(err, ErrTransportAdmissionReplay) {
		t.Fatalf("second consume = %v, want replay", err)
	}
	other, err := MintTransportAdmission(secret, "alice", "global-official", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Consume(other); err != nil {
		t.Fatalf("a different ticket must still consume: %v", err)
	}
}

func TestTransportAdmissionRejectsExcessLifetime(t *testing.T) {
	secret := []byte("transport-admission-test-secret-32-bytes")
	now := time.Unix(1_800_000_000, 0)
	token, err := MintTransportAdmission(
		secret,
		"alice",
		"global-official",
		now.Add(TransportAdmissionMaxLifetime+time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyTransportAdmission(secret, token, now); !errors.Is(err, ErrTransportAdmissionTooFar) {
		t.Fatalf("too-far err = %v, want too-far", err)
	}
}
