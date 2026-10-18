/*
===========================================================================

revocation.go - short-lived, Agent-signed account session revocations

The Agent owns the private session-signing ring; GameWorld receives only its
public projection. A separate signed domain binds the revoked account,
target shard, new minimum session epoch, and short lifetime so ordinary
account sessions cannot forge revocations.

===========================================================================
*/
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
)

const (
	accountRevocationDomain        = "opensro.account-revocation.v1."
	AccountRevocationLifetime      = time.Minute
	accountRevocationMaxTokenBytes = 1024
)

/*
================
AccountRevocationClaims
================
*/
type AccountRevocationClaims struct {
	KeyID     string `json:"key"`
	ShardID   string `json:"shard"`
	AccountID string `json:"account"`
	Epoch     uint64 `json:"epoch"`
	Expires   int64  `json:"expires"`
	Nonce     string `json:"nonce"`
}

/*
================
MintAccountRevocation

Mints a short-lived Ed25519-signed revocation notice binding shardID,
accountID, and the new minimum session epoch.
================
*/
func (signer *AgentSessionSigner) MintAccountRevocation(
	shardID string,
	accountID string,
	epoch uint64,
	now time.Time,
) (string, error) {
	if !domain.AccountIDValid(accountID) {
		return "", ErrAccountIDInvalid
	}
	if shardID == "" || strings.TrimSpace(shardID) != shardID {
		return "", fmt.Errorf("account revocation requires a shard")
	}
	if epoch < 1 {
		return "", fmt.Errorf("account revocation requires a positive epoch")
	}
	ring, err := signer.source.current(false)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, AgentSessionNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload, err := json.Marshal(AccountRevocationClaims{
		KeyID:     ring.activeKeyID,
		ShardID:   shardID,
		AccountID: accountID,
		Epoch:     epoch,
		Expires:   now.Add(AccountRevocationLifetime).Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
	})
	if err != nil {
		return "", err
	}
	signed := accountRevocationDomain + base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(ring.privateKeys[ring.activeKeyID], []byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

/*
================
VerifyAccountRevocation

Verifies a signed account revocation notice against the trusted public-key
ring at instant now.
================
*/
func (verifier *AgentSessionVerifier) VerifyAccountRevocation(
	token string,
	now time.Time,
) (AccountRevocationClaims, error) {
	invalid := fmt.Errorf("invalid account revocation")
	if len(token) > accountRevocationMaxTokenBytes || !strings.HasPrefix(token, accountRevocationDomain) {
		return AccountRevocationClaims{}, invalid
	}
	parts := strings.Split(strings.TrimPrefix(token, accountRevocationDomain), ".")
	if len(parts) != 2 {
		return AccountRevocationClaims{}, invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccountRevocationClaims{}, invalid
	}
	var claims AccountRevocationClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccountRevocationClaims{}, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccountRevocationClaims{}, invalid
	}
	ring, err := verifier.source.current(false)
	if err != nil {
		return AccountRevocationClaims{}, err
	}
	if _, found := ring.publicKeys[claims.KeyID]; !found {
		ring, err = verifier.source.current(true)
		if err != nil {
			return AccountRevocationClaims{}, err
		}
	}
	key := ring.publicKeys[claims.KeyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, []byte(accountRevocationDomain+parts[0]), signature) {
		return AccountRevocationClaims{}, invalid
	}
	nonce, err := base64.RawURLEncoding.DecodeString(claims.Nonce)
	if err != nil ||
		len(nonce) != AgentSessionNonceBytes ||
		claims.ShardID == "" ||
		strings.TrimSpace(claims.ShardID) != claims.ShardID ||
		!domain.AccountIDValid(claims.AccountID) ||
		claims.Epoch < 1 ||
		claims.Expires <= now.Unix() ||
		claims.Expires > now.Add(AccountRevocationLifetime).Unix() {
		return AccountRevocationClaims{}, invalid
	}
	return claims, nil
}
