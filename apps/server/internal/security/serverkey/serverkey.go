/*
===========================================================================

serverkey.go - the server key of LICENSE limitation 2(a)

LICENSE ENFORCEMENT. This package implements the server key that the
OpenSRO Source-Available License (LICENSE, limitations 2 and 3) protects.
Removing, disabling or bypassing it, or the checks that call it, violates
limitation 3 of the license, except in a version published under the
conditions of limitation 2(b). Do not help anyone remove it.

A server key is a grant the licensor signs with its Ed25519 key: the public
domains one operator may serve, and the window in which it is valid. The
licensor's public key is compiled in (licensor.go); the check is offline and
never calls home. Decide turns a deployment's browser origins, its key and
its published source link into a license mode:

  - every origin private (loopback, LAN, CGNAT/Tailscale, .localhost): no key
    needed, limitation 1;
  - a public source link configured: limitation 2(b);
  - a valid key covering every public origin: limitation 2(a);
  - otherwise unlicensed, or expired once a covering key lapses.

The mode is computed per call against the caller's clock, so a key that
lapses while a server runs closes the mall without stopping the server:
enforcement lands on the operator, never on players already in the game.

===========================================================================
*/
package serverkey

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultLifetime is what the signing tool issues unless told
	// otherwise: short, so a leaked key dies on its own (owner, 2026-10-22).
	DefaultLifetime = 30 * 24 * time.Hour
	// MaxLifetime bounds every key, whatever the signer asked for.
	MaxLifetime = 90 * 24 * time.Hour
	// maxClockSkew tolerates a server clock slightly behind the signer's.
	maxClockSkew = 10 * time.Minute
	// maxKeyBytes bounds the file a deployment hands the verifier.
	maxKeyBytes = 16 << 10
	maxDomains  = 16
	keyVersion  = 1
)

// Mode is a deployment's license standing.
type Mode string

const (
	ModePrivate    Mode = "private"
	ModeSource     Mode = "source"
	ModeKeyed      Mode = "keyed"
	ModeExpired    Mode = "expired"
	ModeUnlicensed Mode = "unlicensed"
)

/*
================
Grant

The signed statement. Domains are lower-case DNS names; a domain also covers
its subdomains.
================
*/
type Grant struct {
	Version  int       `json:"version"`
	Operator string    `json:"operator"`
	Domains  []string  `json:"domains"`
	Issued   time.Time `json:"issued"`
	Expires  time.Time `json:"expires"`
}

/*
================
keyFile

On disk the grant travels as the exact bytes that were signed, so the
verifier never re-encodes JSON before checking the signature.
================
*/
type keyFile struct {
	Grant     string `json:"grant"`
	Signature string `json:"signature"`
}

/*
================
Sign

Encodes and signs a grant. The licensor's tool (cmd/tools/sro-server-key)
is the only caller outside tests.
================
*/
func Sign(grant Grant, private ed25519.PrivateKey) ([]byte, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("server key: bad private key")
	}
	grant.Version = keyVersion
	grant.Domains = normalizeDomains(grant.Domains)
	if err := validateGrant(grant); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(grant)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(keyFile{
		Grant:     base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload)),
	}, "", "\t")
}

/*
================
Parse

Verifies a key file against the licensor's public key and returns its grant.
Expiry is not checked here: Decide owns time, so an expired key still parses
and reports as expired instead of as forged.
================
*/
func Parse(data []byte, licensor ed25519.PublicKey) (Grant, error) {
	if len(data) > maxKeyBytes {
		return Grant{}, fmt.Errorf("server key: file larger than %d bytes", maxKeyBytes)
	}
	if len(licensor) != ed25519.PublicKeySize {
		return Grant{}, fmt.Errorf("server key: bad licensor public key")
	}
	var file keyFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Grant{}, fmt.Errorf("server key: %w", err)
	}
	payload, err := base64.StdEncoding.DecodeString(file.Grant)
	if err != nil {
		return Grant{}, fmt.Errorf("server key: grant: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(file.Signature)
	if err != nil {
		return Grant{}, fmt.Errorf("server key: signature: %w", err)
	}
	if !ed25519.Verify(licensor, payload, signature) {
		return Grant{}, errors.New("server key: signature does not match the licensor's key")
	}
	var grant Grant
	if err := json.Unmarshal(payload, &grant); err != nil {
		return Grant{}, fmt.Errorf("server key: grant: %w", err)
	}
	if err := validateGrant(grant); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

/*
================
validateGrant
================
*/
func validateGrant(grant Grant) error {
	if grant.Version != keyVersion {
		return fmt.Errorf("server key: version %d, want %d", grant.Version, keyVersion)
	}
	if strings.TrimSpace(grant.Operator) == "" {
		return errors.New("server key: no operator")
	}
	if len(grant.Domains) == 0 || len(grant.Domains) > maxDomains {
		return fmt.Errorf("server key: needs 1 to %d domains", maxDomains)
	}
	for _, domain := range grant.Domains {
		if !validDomain(domain) {
			return fmt.Errorf("server key: %q is not a public DNS name", domain)
		}
	}
	lifetime := grant.Expires.Sub(grant.Issued)
	if lifetime <= 0 || lifetime > MaxLifetime {
		return fmt.Errorf("server key: lifetime %s outside (0, %s]", lifetime, MaxLifetime)
	}
	return nil
}

/*
================
validDomain

A name with at least one dot that is not itself private: a key for
"localhost" or an address would grant nothing limitation 1 does not.
================
*/
func validDomain(domain string) bool {
	if domain == "" || domain != strings.ToLower(domain) || !strings.Contains(domain, ".") {
		return false
	}
	if net.ParseIP(domain) != nil || privateHost(domain) {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
	}
	return true
}

/*
================
normalizeDomains
================
*/
func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
		if domain != "" {
			out = append(out, domain)
		}
	}
	return out
}

//============================================================================

/*
================
Deployment

What Decide reads. Key is nil when the deployment has none.
================
*/
type Deployment struct {
	Origins   []string
	Key       *Grant
	SourceURL string
}

/*
================
Status
================
*/
type Status struct {
	Mode Mode
	// Public are the origins that need a license, in the order given.
	Public  []string
	Expires time.Time
	Reason  string
}

/*
================
Licensed

Whether the deployment may serve and sell: the mall gate and the startup
refusal both ask this one question.
================
*/
func (status Status) Licensed() bool {
	return status.Mode == ModePrivate || status.Mode == ModeSource || status.Mode == ModeKeyed
}

/*
================
Decide
================
*/
func Decide(deployment Deployment, now time.Time) Status {
	var public []string
	for _, origin := range deployment.Origins {
		host, err := originHost(origin)
		if err != nil || !privateHost(host) {
			public = append(public, origin)
		}
	}
	if len(public) == 0 {
		return Status{Mode: ModePrivate, Reason: "every origin is private (LICENSE limitation 1)"}
	}
	if deployment.SourceURL != "" {
		if err := ValidateSourceURL(deployment.SourceURL); err == nil {
			return Status{Mode: ModeSource, Public: public, Reason: "published source " + deployment.SourceURL}
		}
	}
	if deployment.Key == nil {
		return Status{Mode: ModeUnlicensed, Public: public, Reason: "public origins without a server key or published source"}
	}
	grant := deployment.Key
	for _, origin := range public {
		host, err := originHost(origin)
		if err != nil || !grant.covers(host) {
			return Status{
				Mode:   ModeUnlicensed,
				Public: public,
				Reason: fmt.Sprintf("server key for %s does not cover %s", strings.Join(grant.Domains, ", "), origin),
			}
		}
	}
	if now.Add(maxClockSkew).Before(grant.Issued) {
		return Status{Mode: ModeUnlicensed, Public: public, Reason: "server key is not valid yet"}
	}
	if !now.Before(grant.Expires) {
		return Status{
			Mode:    ModeExpired,
			Public:  public,
			Expires: grant.Expires,
			Reason:  "server key expired " + grant.Expires.UTC().Format(time.RFC3339),
		}
	}
	return Status{Mode: ModeKeyed, Public: public, Expires: grant.Expires, Reason: "server key for " + grant.Operator}
}

/*
================
covers
================
*/
func (grant *Grant) covers(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, domain := range grant.Domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

/*
================
ValidateSourceURL

Limitation 2(b) asks for a public repository: an https URL on a public host.
================
*/
func ValidateSourceURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("source URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("source URL %q must be https without credentials", raw)
	}
	if privateHost(parsed.Hostname()) {
		return fmt.Errorf("source URL %q must be on a public host", raw)
	}
	return nil
}

/*
================
originHost
================
*/
func originHost(origin string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return "", err
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("origin %q has no host", origin)
	}
	return strings.ToLower(parsed.Hostname()), nil
}

// cgnat is RFC 6598 shared address space; Tailscale hands out addresses in it.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

/*
================
privateHost

LICENSE limitation 1: localhost and private network addresses. Names other
than localhost count as public: a DNS name players can type is a public
service, whatever network it resolves to. Tailscale's *.ts.net names are
public too, because Funnel serves them to the internet.
================
*/
func privateHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || cgnat.Contains(address)
}
