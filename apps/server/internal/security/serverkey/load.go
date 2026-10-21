/*
===========================================================================

load.go - one deployment's license inputs, read from the environment

LICENSE ENFORCEMENT (LICENSE limitation 3). The Agent and the deployer
(sro-nomad) both read a deployment through Load, so the release tool refuses
the same deployment the Agent would refuse at startup, before anything stops.

===========================================================================
*/
package serverkey

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"
)

const (
	// EnvKeyPath names the signed key file; empty means no key.
	EnvKeyPath = "SRO_SERVER_KEY_PATH"
	// EnvSourceURL is the public repository of LICENSE limitation 2(b).
	EnvSourceURL = "SRO_PUBLIC_SOURCE_URL"
)

/*
================
Inputs

A key path and source link as an operator configured them.
================
*/
type Inputs struct {
	KeyPath   string
	SourceURL string
}

/*
================
InputsFromEnv
================
*/
func InputsFromEnv(getenv func(string) string) Inputs {
	return Inputs{
		KeyPath:   strings.TrimSpace(getenv(EnvKeyPath)),
		SourceURL: strings.TrimSpace(getenv(EnvSourceURL)),
	}
}

/*
================
Load

Reads and verifies the key file, when there is one, and checks the source
link's form. A configured key that does not verify is an error, never a
silent fall back to "no key": the operator meant to run licensed.
================
*/
func Load(inputs Inputs, origins []string, licensor func() (ed25519.PublicKey, error)) (Deployment, error) {
	deployment := Deployment{Origins: origins, SourceURL: inputs.SourceURL}
	if inputs.SourceURL != "" {
		if err := ValidateSourceURL(inputs.SourceURL); err != nil {
			return Deployment{}, fmt.Errorf("%s: %w", EnvSourceURL, err)
		}
	}
	if inputs.KeyPath == "" {
		return deployment, nil
	}
	data, err := os.ReadFile(inputs.KeyPath)
	if err != nil {
		return Deployment{}, fmt.Errorf("%s: %w", EnvKeyPath, err)
	}
	public, err := licensor()
	if err != nil {
		return Deployment{}, err
	}
	grant, err := Parse(data, public)
	if err != nil {
		return Deployment{}, fmt.Errorf("%s %s: %w", EnvKeyPath, inputs.KeyPath, err)
	}
	deployment.Key = &grant
	return deployment, nil
}
