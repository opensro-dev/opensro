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

	"opensro.online/server/internal/domain"
)

const (
	transportAdmissionPrefix     = "STA1"
	transportAdmissionNonceBytes = 16
	// TransportAdmissionMaxLifetime bounds both theft exposure and the
	// verifier's one-use replay cache. The normal mint is one minute.
	TransportAdmissionMaxLifetime = 5 * time.Minute
)

var (
	ErrTransportAdmissionMalformed = errors.New("auth: malformed transport admission token")
	ErrTransportAdmissionExpired   = errors.New("auth: transport admission token expired")
	ErrTransportAdmissionForged    = errors.New("auth: transport admission token verification failed")
	ErrTransportAdmissionReplay    = errors.New("auth: transport admission token already used")
	ErrTransportAdmissionTooFar    = errors.New("auth: transport admission token expiry exceeds the accepted lifetime")
)

// TransportAdmissionClaims bind a pre-session ticket to the authenticated
// account and shard selected at title login.
type TransportAdmissionClaims struct {
	AccountID string
	ShardID   string
	ExpiresAt time.Time
}

func transportAdmissionMAC(
	secret []byte,
	accountID string,
	shardID string,
	expiryUnix int64,
	nonce []byte,
) []byte {
	hash := hmac.New(sha256.New, secret)
	fmt.Fprintf(
		hash,
		"%s\n%d\n%s\n%s\n%s",
		transportAdmissionPrefix,
		expiryUnix,
		accountID,
		shardID,
		base64.RawURLEncoding.EncodeToString(nonce),
	)
	return hash.Sum(nil)
}

// MintTransportAdmission creates a short-lived one-use pre-session ticket.
func MintTransportAdmission(
	secret []byte,
	accountID string,
	shardID string,
	expiresAt time.Time,
) (string, error) {
	if err := ValidateSecret(secret); err != nil {
		return "", err
	}
	if !domain.AccountIDValid(accountID) {
		return "", fmt.Errorf("auth: invalid account id")
	}
	if strings.TrimSpace(shardID) == "" || strings.TrimSpace(shardID) != shardID {
		return "", fmt.Errorf("auth: invalid shard id")
	}
	nonce := make([]byte, transportAdmissionNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("auth: generating transport admission nonce: %w", err)
	}
	expiry := expiresAt.Unix()
	mac := transportAdmissionMAC(secret, accountID, shardID, expiry, nonce)
	return strings.Join([]string{
		transportAdmissionPrefix,
		strconv.FormatInt(expiry, 10),
		base64.RawURLEncoding.EncodeToString([]byte(accountID)),
		base64.RawURLEncoding.EncodeToString([]byte(shardID)),
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(mac),
	}, "."), nil
}

type parsedTransportAdmission struct {
	expiry    int64
	accountID string
	shardID   string
	nonce     []byte
	mac       []byte
}

func parseTransportAdmission(token string) (parsedTransportAdmission, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 6 || parts[0] != transportAdmissionPrefix {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	accountBytes, err := decodeCanonicalBase64URL(parts[2])
	if err != nil || !domain.AccountIDValid(string(accountBytes)) {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	shardBytes, err := decodeCanonicalBase64URL(parts[3])
	if err != nil || len(shardBytes) == 0 || strings.TrimSpace(string(shardBytes)) != string(shardBytes) {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	nonce, err := decodeCanonicalBase64URL(parts[4])
	if err != nil || len(nonce) != transportAdmissionNonceBytes {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	mac, err := decodeCanonicalBase64URL(parts[5])
	if err != nil || len(mac) != sha256.Size {
		return parsedTransportAdmission{}, ErrTransportAdmissionMalformed
	}
	return parsedTransportAdmission{
		expiry: expiry, accountID: string(accountBytes), shardID: string(shardBytes), nonce: nonce, mac: mac,
	}, nil
}

// VerifyTransportAdmission authenticates a ticket without consuming it.
func VerifyTransportAdmission(
	secret []byte,
	token string,
	now time.Time,
) (TransportAdmissionClaims, error) {
	if err := ValidateSecret(secret); err != nil {
		return TransportAdmissionClaims{}, err
	}
	parsed, err := parseTransportAdmission(token)
	if err != nil {
		return TransportAdmissionClaims{}, err
	}
	if !hmac.Equal(
		parsed.mac,
		transportAdmissionMAC(secret, parsed.accountID, parsed.shardID, parsed.expiry, parsed.nonce),
	) {
		return TransportAdmissionClaims{}, ErrTransportAdmissionForged
	}
	if now.Unix() > parsed.expiry {
		return TransportAdmissionClaims{}, ErrTransportAdmissionExpired
	}
	if parsed.expiry > now.Add(TransportAdmissionMaxLifetime).Unix() {
		return TransportAdmissionClaims{}, ErrTransportAdmissionTooFar
	}
	return TransportAdmissionClaims{
		AccountID: parsed.accountID,
		ShardID:   parsed.shardID,
		ExpiresAt: time.Unix(parsed.expiry, 0),
	}, nil
}

// TransportAdmissionVerifyFunc verifies a ticket; whether it also
// consumes the ticket is the constructor's contract (see below).
type TransportAdmissionVerifyFunc func(token string) (TransportAdmissionClaims, error)

// TransportAdmissionLedger is the one-use replay cache behind the
// verifiers. Authentication (Verify) and consumption (Consume) are
// separate on purpose: the transport verifies a HELLO ticket when it
// arrives but consumes it only once the session's WELCOME frame has
// been written, so a connection that dies before WELCOME - a blocked
// UDP path, an aborted attempt - never burns the ticket and the client
// can present the same ticket on its fallback transport.
type TransportAdmissionLedger struct {
	secret []byte
	now    func() time.Time

	mu   sync.Mutex
	used map[[sha256.Size]byte]int64
}

func NewTransportAdmissionLedger(
	secret []byte,
	now func() time.Time,
) *TransportAdmissionLedger {
	if now == nil {
		now = time.Now
	}
	return &TransportAdmissionLedger{
		secret: append([]byte(nil), secret...),
		now:    now,
		used:   make(map[[sha256.Size]byte]int64),
	}
}

// Verify authenticates a ticket without consuming it.
func (ledger *TransportAdmissionLedger) Verify(
	token string,
) (TransportAdmissionClaims, error) {
	return VerifyTransportAdmission(ledger.secret, token, ledger.now())
}

// Consume marks a ticket used. Re-consuming a consumed ticket (or one
// whose consumption raced this call) is ErrTransportAdmissionReplay.
func (ledger *TransportAdmissionLedger) Consume(token string) error {
	at := ledger.now()
	claims, err := VerifyTransportAdmission(ledger.secret, token, at)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	for prior, expiry := range ledger.used {
		if at.Unix() > expiry {
			delete(ledger.used, prior)
		}
	}
	if _, replayed := ledger.used[digest]; replayed {
		return ErrTransportAdmissionReplay
	}
	ledger.used[digest] = claims.ExpiresAt.Unix()
	return nil
}

// TransportAdmissionVerifier verifies and consumes each ticket once:
// the HELLO-time form for callers with no later commit point.
func TransportAdmissionVerifier(
	secret []byte,
	now func() time.Time,
) TransportAdmissionVerifyFunc {
	ledger := NewTransportAdmissionLedger(secret, now)
	return func(token string) (TransportAdmissionClaims, error) {
		claims, err := ledger.Verify(token)
		if err != nil {
			return TransportAdmissionClaims{}, err
		}
		if err := ledger.Consume(token); err != nil {
			return TransportAdmissionClaims{}, err
		}
		return claims, nil
	}
}
