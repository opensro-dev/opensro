/*
===========================================================================

serverkey.go - the deployment's server key and published source link

LICENSE ENFORCEMENT (LICENSE limitations 2 and 3). The Agent refuses to
start public and unlicensed (agentserver license.go). The deployer runs the
same decision first, so a release without a valid key stops here, before
any running job is touched, instead of leaving the Agent in a restart loop.

Both inputs live in the state directory and survive releases:
<state-dir>/agent/server-key.json is the signed key (the Agent reads it
there), <state-dir>/public-source-url.txt the 2(b) repository link.

===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opensro.online/server/internal/security/serverkey"
)

const (
	serverKeyFile       = "server-key.json"
	publicSourceURLFile = "public-source-url.txt"
	// serverKeyRenewWarning is how early the deployer starts warning.
	serverKeyRenewWarning = 7 * 24 * time.Hour
)

/*
================
licenseSettings

What the Agent job receives: an empty path or link means none.
================
*/
type licenseSettings struct {
	KeyPath   string
	SourceURL string
}

/*
================
configuredLicense

Reads the state directory's key and link and decides the license against
the deployment's origins. Empty origins (a command that deploys nothing)
skip the decision.
================
*/
func configuredLicense(
	stateDir string,
	origins string,
	licensor func() (ed25519.PublicKey, error),
	now time.Time,
) (licenseSettings, error) {
	var settings licenseSettings
	keyPath := filepath.Join(stateDir, "agent", serverKeyFile)
	if _, err := os.Stat(keyPath); err == nil {
		settings.KeyPath = keyPath
	} else if !os.IsNotExist(err) {
		return licenseSettings{}, fmt.Errorf("server key: %w", err)
	}
	link, err := os.ReadFile(filepath.Join(stateDir, publicSourceURLFile))
	if err == nil {
		settings.SourceURL = strings.TrimSpace(string(link))
	} else if !os.IsNotExist(err) {
		return licenseSettings{}, fmt.Errorf("public source link: %w", err)
	}
	if strings.TrimSpace(origins) == "" {
		return settings, nil
	}
	license, err := serverkey.Load(
		serverkey.Inputs{KeyPath: settings.KeyPath, SourceURL: settings.SourceURL},
		strings.Split(origins, ","),
		licensor,
	)
	if err != nil {
		return licenseSettings{}, err
	}
	status := serverkey.Decide(license, now)
	if !status.Licensed() {
		return licenseSettings{}, fmt.Errorf(
			"license %s: %s; put a server key in %s or a public source link in %s (LICENSE limitation 2)",
			status.Mode,
			status.Reason,
			keyPath,
			filepath.Join(stateDir, publicSourceURLFile),
		)
	}
	if status.Mode == serverkey.ModeKeyed && status.Expires.Sub(now) < serverKeyRenewWarning {
		fmt.Printf("Server key expires %s: renew it soon or the Item Mall closes\n", status.Expires.UTC().Format(time.RFC3339))
	}
	return settings, nil
}

/*
================
jobKeyPath

The server_key_path job variable; empty when there is no key.
================
*/
func (settings licenseSettings) jobKeyPath() string {
	if settings.KeyPath == "" {
		return ""
	}
	return slashPath(settings.KeyPath)
}
