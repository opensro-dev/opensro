/*
===========================================================================

secrets.go - the cluster secrets a deploy publishes to Nomad variables

Reads the cluster-state directory sro-provision-identity fills: the account
catalog, the Agent's session signing ring, the provisioning token and the
optional public API token. desiredVariables (variables.go) decides which
secret reaches which job.

===========================================================================
*/
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/security/auth"
)

/*
================
clusterSecrets
================
*/
type clusterSecrets struct {
	AccountChunks     []string
	SessionPrivate    string
	SessionPublic     string
	ProvisioningToken string
	// PublicAPIToken is "" when the cluster has none yet: the GameWorld then
	// serves the public reads without the privacy write.
	PublicAPIToken string
}

/*
================
loadClusterSecrets
================
*/
func loadClusterSecrets(stateDir string) (clusterSecrets, error) {
	accountsPath := filepath.Join(stateDir, "accounts.json")
	if _, err := auth.Load(accountsPath); err != nil {
		return clusterSecrets{}, fmt.Errorf(
			"account catalog %s: %w",
			accountsPath,
			err,
		)
	}
	accountsJSON, err := os.ReadFile(accountsPath)
	if err != nil {
		return clusterSecrets{}, err
	}
	accountChunks, err := chunkAccountCatalog(accountsJSON)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf(
			"account catalog %s: %w",
			accountsPath,
			err,
		)
	}
	keyRingPath := filepath.Join(
		stateDir,
		auth.AgentSessionPrivateKeyRingFile,
	)
	sessionPrivate, err := os.ReadFile(keyRingPath)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w", keyRingPath, err)
	}
	sessionPublic, err := auth.PublicAgentSessionKeyRing(sessionPrivate)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w", keyRingPath, err)
	}
	// The website holds a copy of this token; sro-provision-identity creates it.
	tokenPath := filepath.Join(stateDir, auth.AgentProvisioningTokenFile)
	tokenPayload, err := os.ReadFile(tokenPath)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w (run sro-provision-identity)", tokenPath, err)
	}
	provisioningToken := strings.TrimSpace(string(tokenPayload))
	if len(provisioningToken) < auth.MinProvisioningTokenBytes {
		return clusterSecrets{}, fmt.Errorf(
			"%s: token is shorter than %d bytes",
			tokenPath,
			auth.MinProvisioningTokenBytes,
		)
	}
	publicAPIToken, err := optionalToken(filepath.Join(stateDir, auth.PublicAPITokenFile))
	if err != nil {
		return clusterSecrets{}, err
	}
	return clusterSecrets{
		AccountChunks:     accountChunks,
		SessionPrivate:    string(sessionPrivate),
		SessionPublic:     string(sessionPublic),
		ProvisioningToken: provisioningToken,
		PublicAPIToken:    publicAPIToken,
	}, nil
}

/*
================
optionalToken

The token at path, or "" when the file does not exist. A present but short
token is refused rather than shipped as a guessable credential.
================
*/
func optionalToken(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	token := strings.TrimSpace(string(payload))
	if len(token) < auth.MinProvisioningTokenBytes {
		return "", fmt.Errorf("%s: token is shorter than %d bytes", path, auth.MinProvisioningTokenBytes)
	}
	return token, nil
}
