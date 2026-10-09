// Package auth owns EnterWorld identity-token minting and verification.
// It is dependency-light by design: a token is an HMAC-SHA256 over
// division+character+release protocol+expiry+a random nonce under one
// GameWorld process key.
// The process creates that key at boot and shares it only between its private
// agentapi mint and transport verifier. It is never stored in Nomad, written
// to disk, or shared across shards. The verifier consumes each valid token
// once inside Hub.SetEnterWorldAuth.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MinimumSecretBytes is the minimum entropy-bearing storage accepted
	// for the HMAC key. Operators should generate 32 random bytes and store
	// them in a text-safe encoding; longer values are accepted.
	MinimumSecretBytes = 32

	// MaxTokenLifetime bounds the verifier's replay cache and the useful
	// life of a stolen bind ticket. The normal agent flow issues one-minute
	// tickets; five minutes leaves operational clock and launch headroom.
	MaxTokenLifetime = 5 * time.Minute
)

// tokenPrefix versions the token layout:
// "SEA4.<expiryUnix>.<protocol>.<b64url nonce>.<b64url mac>". SEA4 binds
// division, character and the release protocol the minting request declared
// (the session's encoders pick their contracts by it), and gives every mint
// a unique nonce so the verifier can consume tickets exactly once. The key is
// process-local and tickets live a minute, so no older layout is accepted.
const (
	tokenPrefix = "SEA4"
	nonceBytes  = 16
	// maxTokenProtocol bounds the protocol claim. Which protocols a server
	// serves is releaseprotocol's to decide at the minting route; 0 means the
	// minting request declared none.
	maxTokenProtocol = 255
)

// DenyCodeUnauthorized is the 0x0007 nativeErrorCode for every auth
// refusal (one code on the wire on purpose — the reason is server-log
// only). Matches the code family REV-5 saw in the seam's gate test.
const DenyCodeUnauthorized uint32 = 0x00A1

// Verification refusals. The gate maps them all onto
// DenyCodeUnauthorized; the sentinels exist for logs and tests.
var (
	ErrMalformed  = errors.New("auth: malformed token")
	ErrExpired    = errors.New("auth: token expired")
	ErrForged     = errors.New("auth: token verification failed")
	ErrWeakSecret = errors.New("auth: EnterWorld secret is too short")
	ErrReplay     = errors.New("auth: token already used")
	ErrTooFar     = errors.New("auth: token expiry exceeds the accepted lifetime")
)

func NewRandomSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// ValidateSecret rejects keys that make offline HMAC guessing needlessly
// cheap. This is a configuration boundary, not a password-strength meter:
// production secrets must be generated randomly, never chosen by a human.
func ValidateSecret(secret []byte) error {
	if len(secret) < MinimumSecretBytes {
		return fmt.Errorf("%w: got %d bytes, need at least %d", ErrWeakSecret, len(secret), MinimumSecretBytes)
	}
	return nil
}

// mac computes the HMAC-SHA256 over the canonical claim string. Divisions
// are case-sensitive store keys; character names bind case-insensitively,
// matching the transport's division:lower(name) bind-key convention.
func computeMAC(secret []byte, divisionID, charName string, protocol int, expiryUnix int64, nonce []byte) []byte {
	h := hmac.New(sha256.New, secret)
	fmt.Fprintf(
		h,
		"%s\n%s\n%s\n%d\n%d\n%s",
		tokenPrefix,
		divisionID,
		strings.ToLower(charName),
		protocol,
		expiryUnix,
		base64.RawURLEncoding.EncodeToString(nonce),
	)
	return h.Sum(nil)
}

// Mint issues a unique token binding divisionID+charName, with no declared
// release protocol (0), until expiresAt (MintProtocol). The GameWorld treats
// an undeclared session as the current protocol.
func Mint(secret []byte, divisionID, charName string, expiresAt time.Time) (string, error) {
	return MintProtocol(secret, divisionID, charName, 0, expiresAt)
}

// MintProtocol issues a unique token binding divisionID+charName+protocol
// until expiresAt:
// "SEA4.<expiryUnix>.<protocol>.<base64url(nonce)>.<base64url(HMAC-SHA256(...))>".
// Under 100 ASCII bytes, far below the wire's MaxAuthTokenLen (512).
func MintProtocol(secret []byte, divisionID, charName string, protocol int, expiresAt time.Time) (string, error) {
	if err := ValidateSecret(secret); err != nil {
		return "", err
	}
	if protocol < 0 || protocol > maxTokenProtocol {
		return "", fmt.Errorf("auth: minting release protocol %d out of range", protocol)
	}
	if strings.TrimSpace(divisionID) == "" {
		return "", errors.New("auth: minting requires a division id")
	}
	if strings.TrimSpace(charName) == "" {
		return "", errors.New("auth: minting requires a character name")
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("auth: minting nonce: %w", err)
	}
	expiry := expiresAt.Unix()
	mac := computeMAC(secret, divisionID, charName, protocol, expiry, nonce)
	return fmt.Sprintf(
		"%s.%d.%d.%s.%s",
		tokenPrefix,
		expiry,
		protocol,
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(mac),
	), nil
}

// Verify checks token against the claimed division and character name at
// instant now (VerifyProtocol). nil means authentic and unexpired.
func Verify(secret []byte, token, divisionID, charName string, now time.Time) error {
	_, err := VerifyProtocol(secret, token, divisionID, charName, now)
	return err
}

// VerifyProtocol checks token against the claimed division and character
// name at instant now and returns the release protocol it binds. MAC
// compares first (constant time), so the expiry and protocol claims are only
// trusted once proven authentic.
func VerifyProtocol(secret []byte, token, divisionID, charName string, now time.Time) (int, error) {
	if err := ValidateSecret(secret); err != nil {
		return 0, err
	}
	claims, err := parseToken(token)
	if err != nil {
		return 0, err
	}
	if !hmac.Equal(claims.mac, computeMAC(secret, divisionID, charName, claims.protocol, claims.expiry, claims.nonce)) {
		return 0, ErrForged
	}
	if now.Unix() > claims.expiry {
		return 0, ErrExpired
	}
	if claims.expiry > now.Add(MaxTokenLifetime).Unix() {
		return 0, ErrTooFar
	}
	return claims.protocol, nil
}

// tokenClaims are the parsed, not yet authenticated, token fields.
type tokenClaims struct {
	expiry   int64
	protocol int
	nonce    []byte
	mac      []byte
}

func parseToken(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != tokenPrefix {
		return tokenClaims{}, ErrMalformed
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return tokenClaims{}, ErrMalformed
	}
	protocol, err := strconv.Atoi(parts[2])
	if err != nil || strconv.Itoa(protocol) != parts[2] || protocol < 0 || protocol > maxTokenProtocol {
		return tokenClaims{}, ErrMalformed
	}
	nonce, err := decodeCanonicalBase64URL(parts[3])
	if err != nil || len(nonce) != nonceBytes {
		return tokenClaims{}, ErrMalformed
	}
	mac, err := decodeCanonicalBase64URL(parts[4])
	if err != nil || len(mac) != sha256.Size {
		return tokenClaims{}, ErrMalformed
	}
	return tokenClaims{expiry: expiry, protocol: protocol, nonce: nonce, mac: mac}, nil
}

// VerifyFunc checks one transport-independent EnterWorld claim and returns
// the release protocol the token binds.
type VerifyFunc func(token, divisionID, characterName string) (int, error)

// Verifier binds a secret and clock into a goroutine-safe verifier.
// Transport adaptation and refusal logging belong to the composition root.
func Verifier(secret []byte, now func() time.Time) VerifyFunc {
	if now == nil {
		now = time.Now
	}
	key := append([]byte(nil), secret...)
	var (
		replayMu sync.Mutex
		used     = make(map[[sha256.Size]byte]int64)
	)
	return func(token, divisionID, characterName string) (int, error) {
		if token == "" {
			return 0, ErrMalformed
		}
		at := now()
		protocol, err := VerifyProtocol(key, token, divisionID, characterName, at)
		if err != nil {
			return 0, err
		}
		claims, err := parseToken(token)
		if err != nil {
			return 0, err
		}
		expiry := claims.expiry
		digest := sha256.Sum256([]byte(token))

		replayMu.Lock()
		defer replayMu.Unlock()
		for prior, priorExpiry := range used {
			if at.Unix() > priorExpiry {
				delete(used, prior)
			}
		}
		if _, replayed := used[digest]; replayed {
			return 0, ErrReplay
		}
		used[digest] = expiry
		return protocol, nil
	}
}
