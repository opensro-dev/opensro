/*
===========================================================================

notice.go - short-lived, operator-signed maintenance announcements

The deployer already owns Agent's private signing ring. GameWorld receives
only its public projection. A separate signed domain binds the exact message,
shard and lifetime; ordinary account sessions cannot authorize announcements.

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
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	noticeDomain        = "opensro.operator-notice.v1."
	NoticeLifetime      = time.Minute
	NoticeMaxTextUnits  = 100
	noticeMaxTokenBytes = 2048
)

/*
================
NoticeClaims
================
*/
type NoticeClaims struct {
	KeyID   string `json:"key"`
	ShardID string `json:"shard"`
	Message string `json:"message"`
	Expires int64  `json:"expires"`
	Nonce   string `json:"nonce"`
}

/*
================
ValidateNoticeText

The existing native notification encoder accepts at most 100 UTF-16 units.
Reject malformed or control text before it reaches that encoder.
================
*/
func ValidateNoticeText(message string) error {
	if !utf8.ValidString(message) || strings.TrimSpace(message) == "" || len(utf16.Encode([]rune(message))) > NoticeMaxTextUnits {
		return fmt.Errorf("notice must contain 1..%d UTF-16 text units", NoticeMaxTextUnits)
	}
	for _, value := range message {
		if unicode.IsControl(value) {
			return fmt.Errorf("notice must not contain control characters")
		}
	}
	return nil
}

/*
================
MintNotice
================
*/
func (signer *AgentSessionSigner) MintNotice(shardID, message string, now time.Time) (string, error) {
	if err := ValidateNoticeText(message); err != nil {
		return "", err
	}
	if shardID == "" || strings.TrimSpace(shardID) != shardID {
		return "", fmt.Errorf("notice requires a shard")
	}
	ring, err := signer.source.current(false)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, AgentSessionNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload, err := json.Marshal(NoticeClaims{
		KeyID: ring.activeKeyID, ShardID: shardID, Message: message,
		Expires: now.Add(NoticeLifetime).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonce),
	})
	if err != nil {
		return "", err
	}
	signed := noticeDomain + base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(ring.privateKeys[ring.activeKeyID], []byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

/*
================
VerifyNotice
================
*/
func (verifier *AgentSessionVerifier) VerifyNotice(token string, now time.Time) (NoticeClaims, error) {
	invalid := fmt.Errorf("invalid operator notice")
	if len(token) > noticeMaxTokenBytes || !strings.HasPrefix(token, noticeDomain) {
		return NoticeClaims{}, invalid
	}
	parts := strings.Split(strings.TrimPrefix(token, noticeDomain), ".")
	if len(parts) != 2 {
		return NoticeClaims{}, invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return NoticeClaims{}, invalid
	}
	var claims NoticeClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return NoticeClaims{}, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return NoticeClaims{}, invalid
	}
	ring, err := verifier.source.current(false)
	if err != nil {
		return NoticeClaims{}, err
	}
	if _, found := ring.publicKeys[claims.KeyID]; !found {
		ring, err = verifier.source.current(true)
		if err != nil {
			return NoticeClaims{}, err
		}
	}
	key := ring.publicKeys[claims.KeyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, []byte(noticeDomain+parts[0]), signature) {
		return NoticeClaims{}, invalid
	}
	nonce, err := base64.RawURLEncoding.DecodeString(claims.Nonce)
	if err != nil || len(nonce) != AgentSessionNonceBytes || claims.ShardID == "" || claims.Expires <= now.Unix() || claims.Expires > now.Add(NoticeLifetime).Unix() {
		return NoticeClaims{}, invalid
	}
	if err := ValidateNoticeText(claims.Message); err != nil {
		return NoticeClaims{}, err
	}
	return claims, nil
}
