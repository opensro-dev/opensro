// Package clusterprovision creates missing cluster prerequisites without
// replacing existing credentials or world authority. Production and
// development commands share these primitives so durability and validation
// rules cannot drift.
package clusterprovision

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/privatepath"
	"opensro.online/server/internal/security/auth"
)

// FileResult reports whether one required file was created or preserved.
type FileResult struct {
	Path    string
	Created bool
}

// EnsureIdentity creates Agent's private Ed25519 signing key ring. GameWorld
// processes receive only the public projection through Nomad Variables.
func EnsureIdentity(stateDir string) (FileResult, error) {
	path := filepath.Join(stateDir, auth.AgentSessionPrivateKeyRingFile)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return FileResult{}, fmt.Errorf(
				"existing Agent session key ring is not a regular file",
			)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return FileResult{}, err
		}
		if _, err := auth.PublicAgentSessionKeyRing(payload); err != nil {
			return FileResult{}, fmt.Errorf(
				"existing Agent session key ring: %w",
				err,
			)
		}
		if err := privatepath.ProtectFile(path); err != nil {
			return FileResult{}, fmt.Errorf(
				"protect existing Agent session key ring: %w",
				err,
			)
		}
		return FileResult{Path: path}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileResult{}, err
	}
	payload, err := auth.GenerateAgentSessionKeyRing(time.Now())
	if err != nil {
		return FileResult{}, err
	}
	if err := store.WriteNewFileAtomic(path, payload); err != nil {
		return FileResult{}, err
	}
	if err := privatepath.ProtectFile(path); err != nil {
		return FileResult{}, fmt.Errorf(
			"protect Agent session key ring: %w",
			err,
		)
	}
	return FileResult{Path: path, Created: true}, nil
}

// EnsureProvisioningToken creates the bearer token for the Agent account
// provisioning API: 32 random bytes, hex encoded. An existing token is
// validated and preserved, since the website holds a copy of it.
func EnsureProvisioningToken(stateDir string) (FileResult, error) {
	return ensureBearerToken(filepath.Join(stateDir, auth.AgentProvisioningTokenFile), "provisioning token")
}

// EnsurePublicAPIToken creates the GameWorld public API's privacy-write
// token the same way; the website holds a copy of it too.
func EnsurePublicAPIToken(stateDir string) (FileResult, error) {
	return ensureBearerToken(filepath.Join(stateDir, auth.PublicAPITokenFile), "public API token")
}

// ensureBearerToken creates a 32-byte hex token at path, or validates and
// preserves the one already there.
func ensureBearerToken(path, label string) (FileResult, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return FileResult{}, fmt.Errorf("existing %s is not a regular file", label)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return FileResult{}, err
		}
		if len(strings.TrimSpace(string(payload))) < auth.MinProvisioningTokenBytes {
			return FileResult{}, fmt.Errorf(
				"existing %s is shorter than %d bytes",
				label,
				auth.MinProvisioningTokenBytes,
			)
		}
		if err := privatepath.ProtectFile(path); err != nil {
			return FileResult{}, fmt.Errorf("protect existing %s: %w", label, err)
		}
		return FileResult{Path: path}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileResult{}, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return FileResult{}, err
	}
	if err := store.WriteNewFileAtomic(path, []byte(hex.EncodeToString(secret)+"\n")); err != nil {
		return FileResult{}, err
	}
	if err := privatepath.ProtectFile(path); err != nil {
		return FileResult{}, fmt.Errorf("protect %s: %w", label, err)
	}
	return FileResult{Path: path, Created: true}, nil
}

// EnsureDevelopmentAccount creates a one-row bcrypt catalog when the target
// is absent. An existing catalog is preserved only when the requested account
// and password already authenticate, making reruns safe without silently
// replacing a real operator-owned credential file.
func EnsureDevelopmentAccount(
	path string,
	accountID string,
	password []byte,
) (FileResult, error) {
	if !domain.AccountIDValid(accountID) {
		return FileResult{}, fmt.Errorf("account id %q is invalid", accountID)
	}
	if accountID == domain.ReservedAccountID {
		return FileResult{}, fmt.Errorf("account id %q is reserved", accountID)
	}
	if len(password) == 0 || len(password) > 72 {
		return FileResult{}, fmt.Errorf(
			"password length %d is outside bcrypt's 1..72 byte range",
			len(password),
		)
	}

	if _, err := os.Lstat(path); err == nil {
		if err := validateDevelopmentAccount(path, accountID, password); err != nil {
			return FileResult{}, err
		}
		if err := privatepath.ProtectFile(path); err != nil {
			return FileResult{}, fmt.Errorf("protect existing account catalog: %w", err)
		}
		return FileResult{Path: path}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileResult{}, err
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return FileResult{}, fmt.Errorf("hash password: %w", err)
	}
	rows := []struct {
		ID           string `json:"id"`
		PasswordHash string `json:"passwordHash"`
	}{
		{ID: accountID, PasswordHash: string(hash)},
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return FileResult{}, fmt.Errorf("marshal account catalog: %w", err)
	}
	payload = append(payload, '\n')
	if err := store.WriteNewFileAtomic(path, payload); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: path, Created: true}, nil
}

// EnsureDevelopmentShards creates a fresh empty authority for every enabled
// catalog shard that has no database. Existing authorities are opened under
// their real lock and validated as single-shard stores; a running GameWorld,
// corrupt database, or foreign-shard state is refused rather than skipped.
func EnsureDevelopmentShards(
	catalog *shard.Catalog,
	stateRoot string,
) ([]FileResult, error) {
	if catalog == nil {
		return nil, fmt.Errorf("shard catalog is required")
	}

	results := make([]FileResult, 0)
	for _, definition := range catalog.Definitions() {
		if !definition.Enabled {
			continue
		}
		authorityDir := filepath.Join(stateRoot, definition.ID, "authority")
		result, err := ensureDevelopmentShard(authorityDir, definition.ID)
		if err != nil {
			return results, fmt.Errorf("shard %q: %w", definition.ID, err)
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("shard catalog has no enabled development shards")
	}
	return results, nil
}

func ensureDevelopmentShard(authorityDir, shardID string) (FileResult, error) {
	databasePath := filepath.Join(authorityDir, store.DBFileName)
	created := false

	if _, err := os.Lstat(databasePath); errors.Is(err, os.ErrNotExist) {
		if err := store.Initialize(authorityDir); err != nil {
			return FileResult{}, fmt.Errorf("initialize authority: %w", err)
		}
		created = true
	} else if err != nil {
		return FileResult{}, fmt.Errorf("inspect authority database: %w", err)
	}

	authority, err := store.Open(
		authorityDir,
		store.Options{RequireStore: true},
	)
	if err != nil {
		return FileResult{}, fmt.Errorf("open authority: %w", err)
	}
	defer authority.Close()
	if err := authority.ValidateShardState([]string{shardID}); err != nil {
		return FileResult{}, fmt.Errorf("validate authority: %w", err)
	}
	return FileResult{Path: databasePath, Created: created}, nil
}

func validateDevelopmentAccount(
	path string,
	accountID string,
	password []byte,
) error {
	catalog, err := auth.Load(path)
	if err != nil {
		return fmt.Errorf("existing account catalog is invalid: %w", err)
	}
	hash, found := catalog.PasswordHash(accountID)
	if !found {
		return fmt.Errorf(
			"existing account catalog does not contain %q; refusing to replace it",
			accountID,
		)
	}
	if bcrypt.CompareHashAndPassword(hash, password) != nil {
		return fmt.Errorf(
			"existing account %q uses a different password; refusing to replace it",
			accountID,
		)
	}
	return nil
}
