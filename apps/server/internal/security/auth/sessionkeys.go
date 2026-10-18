package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	AgentSessionPrivateKeyRingFile = "agent-session-keys.json"
	agentSessionKeyCheckInterval   = time.Second
	maxAgentSessionKeys            = 8
)

type agentSessionPrivateDocument struct {
	ActiveKeyID string                   `json:"activeKeyId"`
	Keys        []agentSessionPrivateKey `json:"keys"`
}

type agentSessionPrivateKey struct {
	ID          string `json:"id"`
	PrivateKey  string `json:"privateKey"`
	CreatedAt   string `json:"createdAt"`
	RetireAfter string `json:"retireAfter,omitempty"`
}

type agentSessionPublicDocument struct {
	Keys []agentSessionPublicKey `json:"keys"`
}

type agentSessionPublicKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	CreatedAt string `json:"createdAt"`
}

type sessionSigningRing struct {
	activeKeyID string
	privateKeys map[string]ed25519.PrivateKey
	publicKeys  map[string]ed25519.PublicKey
	digest      string
}

type AgentSessionKeyRingStatus struct {
	ActiveKeyID  string
	PendingKeyID string
	PublicDigest string
	KeyIDs       []string
}

// AgentSessionSigner owns a hot-reloadable private key ring. It is safe for
// concurrent login and proxy verification calls.
type AgentSessionSigner struct {
	source *sessionKeyFile
}

// AgentSessionVerifier owns a hot-reloadable public-only key ring.
type AgentSessionVerifier struct {
	source *sessionKeyFile
}

type sessionKeyFile struct {
	path      string
	private   bool
	mu        sync.Mutex
	ring      *sessionSigningRing
	nextCheck time.Time
	modTime   time.Time
	size      int64
}

func NewAgentSessionSigner(path string) (*AgentSessionSigner, error) {
	source := &sessionKeyFile{path: path, private: true}
	if err := source.reload(true); err != nil {
		return nil, err
	}
	return &AgentSessionSigner{source: source}, nil
}

func NewAgentSessionVerifier(path string) (*AgentSessionVerifier, error) {
	source := &sessionKeyFile{path: path}
	if err := source.reload(true); err != nil {
		return nil, err
	}
	return &AgentSessionVerifier{source: source}, nil
}

func (signer *AgentSessionSigner) Mint(
	accountID string,
	shardID string,
	expiresAt time.Time,
) (string, error) {
	return signer.MintWithEpoch(accountID, shardID, expiresAt, 0)
}

/*
================
MintWithEpoch

Signs a SAS2 bearer token carrying the account's current session epoch.
================
*/
func (signer *AgentSessionSigner) MintWithEpoch(
	accountID string,
	shardID string,
	expiresAt time.Time,
	epoch uint64,
) (string, error) {
	ring, err := signer.source.current(false)
	if err != nil {
		return "", err
	}
	privateKey := ring.privateKeys[ring.activeKeyID]
	return MintAgentSessionWithEpoch(
		ring.activeKeyID,
		privateKey,
		accountID,
		shardID,
		expiresAt,
		epoch,
	)
}

func (signer *AgentSessionSigner) Verify(
	token string,
	now time.Time,
) (AgentSessionClaims, error) {
	return verifyWithSource(signer.source, token, now)
}

func (verifier *AgentSessionVerifier) Verify(
	token string,
	now time.Time,
) (AgentSessionClaims, error) {
	return verifyWithSource(verifier.source, token, now)
}

func verifyWithSource(
	source *sessionKeyFile,
	token string,
	now time.Time,
) (AgentSessionClaims, error) {
	keyID, err := agentSessionKeyID(token)
	if err != nil {
		return AgentSessionClaims{}, err
	}
	ring, err := source.current(false)
	if err != nil {
		return AgentSessionClaims{}, err
	}
	if _, found := ring.publicKeys[keyID]; !found {
		if ring, err = source.current(true); err != nil {
			return AgentSessionClaims{}, err
		}
	}
	return VerifyAgentSession(ring.publicKeys, token, now)
}

func (signer *AgentSessionSigner) Digest() (string, error) {
	ring, err := signer.source.current(false)
	if err != nil {
		return "", err
	}
	return ring.digest, nil
}

func (signer *AgentSessionSigner) ActiveKeyID() (string, error) {
	ring, err := signer.source.current(false)
	if err != nil {
		return "", err
	}
	return ring.activeKeyID, nil
}

func (verifier *AgentSessionVerifier) Digest() (string, error) {
	ring, err := verifier.source.current(false)
	if err != nil {
		return "", err
	}
	return ring.digest, nil
}

func (source *sessionKeyFile) current(force bool) (*sessionSigningRing, error) {
	if err := source.reload(force); err != nil {
		return nil, err
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.ring, nil
}

func (source *sessionKeyFile) reload(force bool) error {
	source.mu.Lock()
	defer source.mu.Unlock()

	now := time.Now()
	if !force && now.Before(source.nextCheck) {
		return nil
	}
	source.nextCheck = now.Add(agentSessionKeyCheckInterval)

	info, err := os.Stat(source.path)
	if err != nil {
		return fmt.Errorf("agent session key ring %s: %w", source.path, err)
	}
	if !force &&
		source.ring != nil &&
		info.Size() == source.size &&
		info.ModTime().Equal(source.modTime) {
		return nil
	}
	payload, err := os.ReadFile(source.path)
	if err != nil {
		return fmt.Errorf("read agent session key ring %s: %w", source.path, err)
	}
	var ring *sessionSigningRing
	if source.private {
		ring, err = parsePrivateSessionKeys(payload)
	} else {
		ring, err = parsePublicSessionKeys(payload)
	}
	if err != nil {
		return fmt.Errorf("agent session key ring %s: %w", source.path, err)
	}
	source.ring = ring
	source.size = info.Size()
	source.modTime = info.ModTime()
	return nil
}

func GenerateAgentSessionKeyRing(now time.Time) ([]byte, error) {
	key, err := generateAgentSessionPrivateKey(now)
	if err != nil {
		return nil, err
	}
	document := agentSessionPrivateDocument{
		ActiveKeyID: key.ID,
		Keys:        []agentSessionPrivateKey{key},
	}
	return marshalSessionKeyDocument(document)
}

// PrepareAgentSessionKeyRotation appends one unpublished signing key while
// leaving the active key unchanged. Repeating the call returns the same
// pending key, which makes an interrupted publish phase resumable.
func PrepareAgentSessionKeyRotation(
	privatePayload []byte,
	now time.Time,
) ([]byte, string, error) {
	document, err := privateSessionKeyDocument(privatePayload)
	if err != nil {
		return nil, "", err
	}
	for _, key := range document.Keys {
		if key.ID != document.ActiveKeyID && key.RetireAfter == "" {
			return append([]byte(nil), privatePayload...), key.ID, nil
		}
	}
	document.Keys = retainSessionKeys(document, now)
	if len(document.Keys) >= maxAgentSessionKeys {
		return nil, "", fmt.Errorf(
			"agent session key ring contains %d keys; retire expired keys first",
			len(document.Keys),
		)
	}
	key, err := generateAgentSessionPrivateKey(now)
	if err != nil {
		return nil, "", err
	}
	document.Keys = append(document.Keys, key)
	payload, err := marshalSessionKeyDocument(document)
	if err != nil {
		return nil, "", err
	}
	if _, err := parsePrivateSessionKeys(payload); err != nil {
		return nil, "", err
	}
	return payload, key.ID, nil
}

// ActivateAgentSessionKey switches minting only after every verifier has
// acknowledged the prepared public ring. The previous active key remains
// verifiable for the maximum accepted token lifetime.
func ActivateAgentSessionKey(
	privatePayload []byte,
	keyID string,
	now time.Time,
) ([]byte, error) {
	document, err := privateSessionKeyDocument(privatePayload)
	if err != nil {
		return nil, err
	}
	if keyID == document.ActiveKeyID {
		return append([]byte(nil), privatePayload...), nil
	}
	found := false
	for index := range document.Keys {
		key := &document.Keys[index]
		switch key.ID {
		case keyID:
			if key.RetireAfter != "" {
				return nil, fmt.Errorf("key %q is already retiring", keyID)
			}
			found = true
		case document.ActiveKeyID:
			key.RetireAfter = now.UTC().
				Add(AgentSessionMaxLifetime).
				Format(time.RFC3339)
		}
	}
	if !found {
		return nil, fmt.Errorf("pending key %q is absent", keyID)
	}
	document.ActiveKeyID = keyID
	payload, err := marshalSessionKeyDocument(document)
	if err != nil {
		return nil, err
	}
	if _, err := parsePrivateSessionKeys(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// RetireExpiredAgentSessionKeys removes only keys whose overlap deadline has
// passed. A pending key is never removed implicitly.
func RetireExpiredAgentSessionKeys(
	privatePayload []byte,
	now time.Time,
) ([]byte, []string, error) {
	document, err := privateSessionKeyDocument(privatePayload)
	if err != nil {
		return nil, nil, err
	}
	retained := retainSessionKeys(document, now)
	removed := make([]string, 0, len(document.Keys)-len(retained))
	keep := make(map[string]struct{}, len(retained))
	for _, key := range retained {
		keep[key.ID] = struct{}{}
	}
	for _, key := range document.Keys {
		if _, found := keep[key.ID]; !found {
			removed = append(removed, key.ID)
		}
	}
	document.Keys = retained
	payload, err := marshalSessionKeyDocument(document)
	if err != nil {
		return nil, nil, err
	}
	if _, err := parsePrivateSessionKeys(payload); err != nil {
		return nil, nil, err
	}
	return payload, removed, nil
}

func PublicAgentSessionKeyRing(privatePayload []byte) ([]byte, error) {
	ring, err := parsePrivateSessionKeys(privatePayload)
	if err != nil {
		return nil, err
	}
	var source agentSessionPrivateDocument
	if err := decodeSessionKeyDocument(privatePayload, &source); err != nil {
		return nil, err
	}
	created := make(map[string]string, len(source.Keys))
	for _, key := range source.Keys {
		created[key.ID] = key.CreatedAt
	}
	document := agentSessionPublicDocument{
		Keys: make([]agentSessionPublicKey, 0, len(ring.publicKeys)),
	}
	for _, key := range source.Keys {
		publicKey := ring.publicKeys[key.ID]
		document.Keys = append(document.Keys, agentSessionPublicKey{
			ID: key.ID,
			PublicKey: base64.RawURLEncoding.EncodeToString(
				publicKey,
			),
			CreatedAt: created[key.ID],
		})
	}
	return marshalSessionKeyDocument(document)
}

func AgentSessionPublicKeyDigest(privatePayload []byte) (string, error) {
	ring, err := parsePrivateSessionKeys(privatePayload)
	if err != nil {
		return "", err
	}
	return ring.digest, nil
}

func AgentSessionActiveKeyID(privatePayload []byte) (string, error) {
	ring, err := parsePrivateSessionKeys(privatePayload)
	if err != nil {
		return "", err
	}
	return ring.activeKeyID, nil
}

func InspectAgentSessionKeyRing(
	privatePayload []byte,
) (AgentSessionKeyRingStatus, error) {
	ring, err := parsePrivateSessionKeys(privatePayload)
	if err != nil {
		return AgentSessionKeyRingStatus{}, err
	}
	var document agentSessionPrivateDocument
	if err := decodeSessionKeyDocument(privatePayload, &document); err != nil {
		return AgentSessionKeyRingStatus{}, err
	}
	status := AgentSessionKeyRingStatus{
		ActiveKeyID:  ring.activeKeyID,
		PublicDigest: ring.digest,
		KeyIDs:       make([]string, 0, len(ring.publicKeys)),
	}
	for _, key := range document.Keys {
		status.KeyIDs = append(status.KeyIDs, key.ID)
		if key.ID != document.ActiveKeyID && key.RetireAfter == "" {
			status.PendingKeyID = key.ID
		}
	}
	sort.Strings(status.KeyIDs)
	return status, nil
}

func generateAgentSessionPrivateKey(now time.Time) (
	agentSessionPrivateKey,
	error,
) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return agentSessionPrivateKey{}, fmt.Errorf(
			"generate Agent session key: %w",
			err,
		)
	}
	keyID := sessionKeyID(publicKey)
	return agentSessionPrivateKey{
		ID: keyID,
		PrivateKey: base64.RawURLEncoding.EncodeToString(
			privateKey,
		),
		CreatedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

func privateSessionKeyDocument(
	payload []byte,
) (agentSessionPrivateDocument, error) {
	if _, err := parsePrivateSessionKeys(payload); err != nil {
		return agentSessionPrivateDocument{}, err
	}
	var document agentSessionPrivateDocument
	if err := decodeSessionKeyDocument(payload, &document); err != nil {
		return agentSessionPrivateDocument{}, err
	}
	return document, nil
}

func retainSessionKeys(
	document agentSessionPrivateDocument,
	now time.Time,
) []agentSessionPrivateKey {
	keys := make([]agentSessionPrivateKey, 0, len(document.Keys))
	for _, key := range document.Keys {
		if key.ID == document.ActiveKeyID || key.RetireAfter == "" {
			keys = append(keys, key)
			continue
		}
		retireAfter, err := time.Parse(time.RFC3339, key.RetireAfter)
		if err != nil || now.Before(retireAfter) {
			keys = append(keys, key)
		}
	}
	return keys
}

func parsePrivateSessionKeys(payload []byte) (*sessionSigningRing, error) {
	var document agentSessionPrivateDocument
	if err := decodeSessionKeyDocument(payload, &document); err != nil {
		return nil, err
	}
	if !agentSessionKeyIDValid(document.ActiveKeyID) {
		return nil, fmt.Errorf("invalid active key id")
	}
	privateKeys := make(map[string]ed25519.PrivateKey, len(document.Keys))
	publicKeys := make(map[string]ed25519.PublicKey, len(document.Keys))
	pendingKeys := 0
	if len(document.Keys) == 0 || len(document.Keys) > maxAgentSessionKeys {
		return nil, fmt.Errorf(
			"key count %d is outside 1..%d",
			len(document.Keys),
			maxAgentSessionKeys,
		)
	}
	for _, encoded := range document.Keys {
		if _, duplicate := privateKeys[encoded.ID]; duplicate {
			return nil, fmt.Errorf("duplicate key id %q", encoded.ID)
		}
		if _, err := time.Parse(time.RFC3339, encoded.CreatedAt); err != nil {
			return nil, fmt.Errorf("key %q createdAt: %w", encoded.ID, err)
		}
		if encoded.RetireAfter != "" {
			retireAfter, err := time.Parse(time.RFC3339, encoded.RetireAfter)
			if err != nil {
				return nil, fmt.Errorf(
					"key %q retireAfter: %w",
					encoded.ID,
					err,
				)
			}
			createdAt, _ := time.Parse(time.RFC3339, encoded.CreatedAt)
			if !retireAfter.After(createdAt) {
				return nil, fmt.Errorf(
					"key %q retirement does not follow creation",
					encoded.ID,
				)
			}
		} else if encoded.ID != document.ActiveKeyID {
			pendingKeys++
		}
		privateKey, err := base64.RawURLEncoding.DecodeString(
			encoded.PrivateKey,
		)
		if err != nil || len(privateKey) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("key %q has invalid private material", encoded.ID)
		}
		publicKey := privateKey[ed25519.SeedSize:]
		if encoded.ID != sessionKeyID(publicKey) {
			return nil, fmt.Errorf("key %q id does not match its public key", encoded.ID)
		}
		privateKeys[encoded.ID] = ed25519.PrivateKey(
			append([]byte(nil), privateKey...),
		)
		publicKeys[encoded.ID] = ed25519.PublicKey(
			append([]byte(nil), publicKey...),
		)
	}
	if _, found := privateKeys[document.ActiveKeyID]; !found {
		return nil, fmt.Errorf("active key %q is absent", document.ActiveKeyID)
	}
	if pendingKeys > 1 {
		return nil, fmt.Errorf("key ring has %d pending keys", pendingKeys)
	}
	return &sessionSigningRing{
		activeKeyID: document.ActiveKeyID,
		privateKeys: privateKeys,
		publicKeys:  publicKeys,
		digest:      publicKeyDigest(publicKeys),
	}, nil
}

func parsePublicSessionKeys(payload []byte) (*sessionSigningRing, error) {
	var document agentSessionPublicDocument
	if err := decodeSessionKeyDocument(payload, &document); err != nil {
		return nil, err
	}
	if len(document.Keys) == 0 || len(document.Keys) > maxAgentSessionKeys {
		return nil, fmt.Errorf(
			"key count %d is outside 1..%d",
			len(document.Keys),
			maxAgentSessionKeys,
		)
	}
	publicKeys := make(map[string]ed25519.PublicKey, len(document.Keys))
	for _, encoded := range document.Keys {
		if _, duplicate := publicKeys[encoded.ID]; duplicate {
			return nil, fmt.Errorf("duplicate key id %q", encoded.ID)
		}
		if _, err := time.Parse(time.RFC3339, encoded.CreatedAt); err != nil {
			return nil, fmt.Errorf("key %q createdAt: %w", encoded.ID, err)
		}
		publicKey, err := base64.RawURLEncoding.DecodeString(
			encoded.PublicKey,
		)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("key %q has invalid public material", encoded.ID)
		}
		if encoded.ID != sessionKeyID(publicKey) {
			return nil, fmt.Errorf("key %q id does not match its public key", encoded.ID)
		}
		publicKeys[encoded.ID] = ed25519.PublicKey(
			append([]byte(nil), publicKey...),
		)
	}
	return &sessionSigningRing{
		publicKeys: publicKeys,
		digest:     publicKeyDigest(publicKeys),
	}, nil
}

func decodeSessionKeyDocument(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func marshalSessionKeyDocument(document any) ([]byte, error) {
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func sessionKeyID(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return hex.EncodeToString(digest[:16])
}

func publicKeyDigest(keys map[string]ed25519.PublicKey) string {
	// IDs are hashes of their keys. Hashing the sorted IDs gives operators a
	// compact, non-secret acknowledgement value for staged publication.
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	digest := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(digest[:])
}
