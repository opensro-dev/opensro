/*
===========================================================================

agentsession.go - asymmetric Ed25519 SAS2 title-session tokens

Mints and verifies Agent session tokens (SAS2.<keyId>.<expiry>.<account>.
<shard>.<nonce>.<sig>). The upper 8 bytes of the 16-byte nonce encode the
account's session epoch so account revocation invalidates prior sessions
without changing the SAS2 wire layout.

===========================================================================
*/
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
)

const (
	AgentSessionPrefix      = "SAS2"
	AgentSessionNonceBytes  = 16
	AgentSessionLifetime    = 12 * time.Hour
	AgentSessionMaxLifetime = 13 * time.Hour
)

var (
	ErrAgentSessionMalformed = errors.New("auth: malformed agent session")
	ErrAgentSessionExpired   = errors.New("auth: agent session expired")
	ErrAgentSessionForged    = errors.New("auth: agent session verification failed")
	ErrAgentSessionTooFar    = errors.New("auth: agent session expiry exceeds the accepted lifetime")
	ErrAgentSessionKey       = errors.New("auth: agent session signing key is unavailable")
)

// AgentSessionClaims are the global account and immutable shard selected at
// title login. The token is reusable until expiry; EnterWorld has its own
// short-lived one-use ticket.
type AgentSessionClaims struct {
	AccountID string
	ShardID   string
	ExpiresAt time.Time
	Epoch     uint64
	Nonce     string
}

/*
================
MintAgentSession

Creates a SAS2 token with epoch 0. Only Agent receives private keys;
GameWorld processes verify the signature with a public key ring.
================
*/
func MintAgentSession(
	keyID string,
	privateKey ed25519.PrivateKey,
	accountID string,
	shardID string,
	expiresAt time.Time,
) (string, error) {
	return MintAgentSessionWithEpoch(keyID, privateKey, accountID, shardID, expiresAt, 0)
}

/*
================
MintAgentSessionWithEpoch

Creates a SAS2 token carrying the account's current session epoch in the
upper 8 bytes of the 16-byte nonce and 64 bits of cryptographic randomness
in the lower 8 bytes.
================
*/
func MintAgentSessionWithEpoch(
	keyID string,
	privateKey ed25519.PrivateKey,
	accountID string,
	shardID string,
	expiresAt time.Time,
	epoch uint64,
) (string, error) {
	if !agentSessionKeyIDValid(keyID) ||
		len(privateKey) != ed25519.PrivateKeySize {
		return "", ErrAgentSessionKey
	}
	if !domain.AccountIDValid(accountID) {
		return "", fmt.Errorf("auth: invalid account id")
	}
	if strings.TrimSpace(shardID) == "" ||
		strings.TrimSpace(shardID) != shardID {
		return "", fmt.Errorf("auth: invalid shard id")
	}
	nonce := make([]byte, AgentSessionNonceBytes)
	binary.BigEndian.PutUint64(nonce[:8], epoch)
	if _, err := rand.Read(nonce[8:]); err != nil {
		return "", fmt.Errorf("auth: generating agent session nonce: %w", err)
	}
	parts := []string{
		AgentSessionPrefix,
		keyID,
		strconv.FormatInt(expiresAt.Unix(), 10),
		base64.RawURLEncoding.EncodeToString([]byte(accountID)),
		base64.RawURLEncoding.EncodeToString([]byte(shardID)),
		base64.RawURLEncoding.EncodeToString(nonce),
	}
	signature := ed25519.Sign(
		privateKey,
		[]byte(strings.Join(parts, ".")),
	)
	return strings.Join(append(
		parts,
		base64.RawURLEncoding.EncodeToString(signature),
	), "."), nil
}

/*
================
VerifyAgentSession

Authenticates a SAS2 token with the public key selected by its signed key ID.
The caller still checks the returned shard against the endpoint it owns.
================
*/
func VerifyAgentSession(
	publicKeys map[string]ed25519.PublicKey,
	token string,
	now time.Time,
) (AgentSessionClaims, error) {
	parsed, err := parseAgentSession(token)
	if err != nil {
		return AgentSessionClaims{}, err
	}
	publicKey, found := publicKeys[parsed.keyID]
	if !found || len(publicKey) != ed25519.PublicKeySize {
		return AgentSessionClaims{}, ErrAgentSessionKey
	}
	if !ed25519.Verify(
		publicKey,
		[]byte(strings.Join(parsed.signedParts, ".")),
		parsed.signature,
	) {
		return AgentSessionClaims{}, ErrAgentSessionForged
	}
	if now.Unix() > parsed.expiry {
		return AgentSessionClaims{}, ErrAgentSessionExpired
	}
	if parsed.expiry > now.Add(AgentSessionMaxLifetime).Unix() {
		return AgentSessionClaims{}, ErrAgentSessionTooFar
	}
	return AgentSessionClaims{
		AccountID: parsed.accountID,
		ShardID:   parsed.shardID,
		ExpiresAt: time.Unix(parsed.expiry, 0),
		Epoch:     binary.BigEndian.Uint64(parsed.nonce[:8]),
		Nonce:     parsed.nonceText,
	}, nil
}

type parsedAgentSession struct {
	keyID       string
	expiry      int64
	accountID   string
	shardID     string
	nonce       []byte
	nonceText   string
	signedParts []string
	signature   []byte
}

/*
================
parseAgentSession
================
*/
func parseAgentSession(token string) (parsedAgentSession, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 7 ||
		parts[0] != AgentSessionPrefix ||
		!agentSessionKeyIDValid(parts[1]) {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	accountBytes, err := decodeCanonicalBase64URL(parts[3])
	if err != nil || !domain.AccountIDValid(string(accountBytes)) {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	shardBytes, err := decodeCanonicalBase64URL(parts[4])
	if err != nil ||
		len(shardBytes) == 0 ||
		strings.TrimSpace(string(shardBytes)) != string(shardBytes) {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	nonce, err := decodeCanonicalBase64URL(parts[5])
	if err != nil || len(nonce) != AgentSessionNonceBytes {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	signature, err := decodeCanonicalBase64URL(parts[6])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return parsedAgentSession{}, ErrAgentSessionMalformed
	}
	return parsedAgentSession{
		keyID:       parts[1],
		expiry:      expiry,
		accountID:   string(accountBytes),
		shardID:     string(shardBytes),
		nonce:       nonce,
		nonceText:   parts[5],
		signedParts: parts[:6],
		signature:   signature,
	}, nil
}

func agentSessionKeyID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 7 ||
		parts[0] != AgentSessionPrefix ||
		!agentSessionKeyIDValid(parts[1]) {
		return "", ErrAgentSessionMalformed
	}
	return parts[1], nil
}

func agentSessionKeyIDValid(keyID string) bool {
	if len(keyID) != 32 {
		return false
	}
	for _, character := range keyID {
		if character < '0' || character > '9' &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
