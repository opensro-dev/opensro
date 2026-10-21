/*
===========================================================================

licensor.go - the licensor's public key, compiled into every server

LICENSE ENFORCEMENT (LICENSE limitation 3): changing this key so that keys
the licensor did not sign are accepted circumvents the server key.

The private half never enters this repository. The licensor creates the
pair with `sro-server-key genkey` and keeps the private file offline.

===========================================================================
*/
package serverkey

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// licensorPublicKey is the base64 Ed25519 public key of the OpenSRO
// licensor. Empty until the licensor has generated its key pair.
const licensorPublicKey = ""

/*
================
LicensorKey
================
*/
func LicensorKey() (ed25519.PublicKey, error) {
	if licensorPublicKey == "" {
		return nil, errors.New("server key: this build carries no licensor public key")
	}
	raw, err := base64.StdEncoding.DecodeString(licensorPublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("server key: licensor public key is malformed")
	}
	return ed25519.PublicKey(raw), nil
}
