package auth

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

var (
	testSecret = []byte("test-secret-please-rotate-at-least-32-bytes")
	testNow    = time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)
)

const testDivision = "d1"

func mintOK(t *testing.T, charName string, expiresAt time.Time) string {
	t.Helper()
	token, err := Mint(testSecret, testDivision, charName, expiresAt)
	if err != nil {
		t.Fatalf("mint failed: %v", err)
	}
	return token
}

// TestMintVerifyRoundtrip is the core proof: mint -> verify passes for the
// bound character (case-insensitively, matching the bind-key convention),
// stays under the wire cap, and refuses every other character.
func TestMintVerifyRoundtrip(t *testing.T) {
	token := mintOK(t, "asd2", testNow.Add(time.Minute))

	if len(token) > transport.MaxAuthTokenLen {
		t.Fatalf("token %d bytes exceeds wire cap %d", len(token), transport.MaxAuthTokenLen)
	}
	if err := Verify(testSecret, token, testDivision, "asd2", testNow); err != nil {
		t.Fatalf("roundtrip refused: %v", err)
	}
	if err := Verify(testSecret, token, testDivision, "ASD2", testNow); err != nil {
		t.Fatalf("case-insensitive verify refused: %v", err)
	}
	if err := Verify(testSecret, token, testDivision, "somebodyelse", testNow); !errors.Is(err, ErrForged) {
		t.Fatalf("wrong character = %v, want ErrForged", err)
	}
	if err := Verify(testSecret, token, "other-division", "asd2", testNow); !errors.Is(err, ErrForged) {
		t.Fatalf("wrong division = %v, want ErrForged", err)
	}
}

// TestVerifyExpired: authentic but stale tokens refuse with ErrExpired;
// the same instant as the expiry second still passes (<= boundary).
func TestVerifyExpired(t *testing.T) {
	token := mintOK(t, "asd2", testNow.Add(-time.Hour))
	if err := Verify(testSecret, token, testDivision, "asd2", testNow); !errors.Is(err, ErrExpired) {
		t.Fatalf("stale token = %v, want ErrExpired", err)
	}

	boundary := mintOK(t, "asd2", testNow)
	if err := Verify(testSecret, boundary, testDivision, "asd2", testNow); err != nil {
		t.Fatalf("boundary-second token refused: %v", err)
	}
	if err := Verify(testSecret, boundary, testDivision, "asd2", testNow.Add(time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("one-past-boundary = %v, want ErrExpired", err)
	}
}

// TestVerifyForgedAndMalformed: tampered MACs, wrong secrets, altered
// claims, and garbage shapes all refuse. An expiry tampered to the future
// must read as FORGED, not expired-then-honored.
func TestVerifyForgedAndMalformed(t *testing.T) {
	token := mintOK(t, "asd2", testNow.Add(time.Minute))

	// Replace the MAC's last two digits with ones that differ: a random MAC
	// already ending in "AA" made the old "AA" tamper a no-op (1 in 4096).
	// The final "A" keeps the digit canonical (its unused bits stay zero).
	suffix := "AA"
	if strings.HasSuffix(token, suffix) {
		suffix = "BA"
	}
	tampered := token[:len(token)-2] + suffix
	if err := Verify(testSecret, tampered, testDivision, "asd2", testNow); !errors.Is(err, ErrForged) {
		t.Errorf("tampered mac = %v, want ErrForged", err)
	}
	if err := Verify([]byte("other-secret-also-at-least-32-bytes-long"), token, testDivision, "asd2", testNow); !errors.Is(err, ErrForged) {
		t.Errorf("wrong secret = %v, want ErrForged", err)
	}

	// Stretch the expiry claim without re-signing: forged.
	parts := strings.Split(token, ".")
	stretched := strings.Join([]string{parts[0], "9999999999", parts[2], parts[3]}, ".")
	if err := Verify(testSecret, stretched, testDivision, "asd2", testNow); !errors.Is(err, ErrForged) {
		t.Errorf("stretched expiry = %v, want ErrForged", err)
	}

	for _, bad := range []string{
		"", "garbage", "SEA3", "SEA3.123", "SEA3.123.%%%", "SEA2.123.AAAA",
		"SEA3.notanumber." + parts[2] + "." + parts[3],
		"SEA3.123.AAAA.AAAA", token + ".extra",
	} {
		if err := Verify(testSecret, bad, testDivision, "asd2", testNow); !errors.Is(err, ErrMalformed) {
			t.Errorf("Verify(%q) = %v, want ErrMalformed", bad, err)
		}
	}
}

func TestVerifyRejectsNonCanonicalBase64URL(t *testing.T) {
	token := mintOK(t, "asd2", testNow.Add(time.Minute))
	parts := strings.Split(token, ".")

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := len(parts[3]) - 1
	index := strings.IndexByte(alphabet, parts[3][last])
	if index < 0 {
		t.Fatal("minted MAC is not base64url")
	}

	// A 32-byte MAC leaves four unused bits in its final raw-base64 digit.
	// Toggle one of them: the decoded bytes stay identical, but the spelling
	// is non-canonical and must not become a second identity for one bearer.
	parts[3] = parts[3][:last] + string(alphabet[index^1])

	if err := Verify(
		testSecret,
		strings.Join(parts, "."),
		testDivision,
		"asd2",
		testNow,
	); !errors.Is(err, ErrMalformed) {
		t.Fatalf("non-canonical token = %v, want ErrMalformed", err)
	}
}

// TestVerifierPolicy drives the gate closure exactly as Hub.dispatch
// does: good tokens bind while bad, expired, and absent tokens refuse
// with one deny code.
func TestVerifierPolicy(t *testing.T) {
	now := func() time.Time { return testNow }
	good := mintOK(t, "asd2", testNow.Add(time.Minute))
	stale := mintOK(t, "asd2", testNow.Add(-time.Minute))

	verifier := Verifier(testSecret, now)

	check := func(name, token string, want error) {
		t.Helper()
		err := verifier(token, testDivision, "asd2")
		if !errors.Is(err, want) {
			t.Errorf("%s: error = %v, want %v", name, err, want)
		}
	}

	check("good", good, nil)
	check("forged", "SEA3.99.AAAA.AAAA", ErrMalformed)
	check("stale", stale, ErrExpired)
	check("tokenless", "", ErrMalformed)
	check("replay", good, ErrReplay)
}

func TestSecretMinimum(t *testing.T) {
	weak := []byte("human-chosen-secret")
	if _, err := Mint(weak, testDivision, "asd2", testNow.Add(time.Minute)); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("Mint weak secret = %v, want ErrWeakSecret", err)
	}
	if err := Verify(weak, "SEA3.1.AAAA.AAAA", testDivision, "asd2", testNow); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("Verify weak secret = %v, want ErrWeakSecret", err)
	}
}

func TestVerifyRejectsExcessiveLifetime(t *testing.T) {
	token := mintOK(t, "asd2", testNow.Add(MaxTokenLifetime+time.Second))
	if err := Verify(testSecret, token, testDivision, "asd2", testNow); !errors.Is(err, ErrTooFar) {
		t.Fatalf("long-lived token = %v, want ErrTooFar", err)
	}
}

func TestMintUsesUniqueNonces(t *testing.T) {
	first := mintOK(t, "asd2", testNow.Add(time.Minute))
	second := mintOK(t, "asd2", testNow.Add(time.Minute))
	if first == second {
		t.Fatal("two token mints produced the same bearer")
	}
}

// TestVerifierConcurrent is REV-4 702's goroutine-safety gate: the gate
// closure runs from every session's read loop concurrently; hammer one
// verifier from many goroutines under -race.
func TestVerifierConcurrent(t *testing.T) {
	now := func() time.Time { return testNow }
	fn := Verifier(testSecret, now)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				good := mintOK(t, "asd2", testNow.Add(time.Minute))
				if err := fn(good, testDivision, "asd2"); err != nil {
					t.Errorf("goroutine %d: good token refused", g)
					return
				}
				if err := fn("SEA3.1.AAAA.AAAA", testDivision, "asd2"); err == nil {
					t.Errorf("goroutine %d: forged token accepted", g)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
