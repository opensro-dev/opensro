/*
===========================================================================

serverkey_test.go - signing, verification and the license decision

===========================================================================
*/
package serverkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

/*
================
testLicensor
================
*/
func testLicensor(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

/*
================
testGrant
================
*/
func testGrant(domains ...string) Grant {
	return Grant{Operator: "official", Domains: domains, Issued: testNow, Expires: testNow.Add(DefaultLifetime)}
}

/*
================
TestSignParseRoundTrip
================
*/
func TestSignParseRoundTrip(t *testing.T) {
	public, private := testLicensor(t)
	data, err := Sign(testGrant("OpenSRO.online."), private)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := Parse(data, public)
	if err != nil {
		t.Fatal(err)
	}
	if len(grant.Domains) != 1 || grant.Domains[0] != "opensro.online" || grant.Operator != "official" {
		t.Fatalf("grant = %+v", grant)
	}
}

/*
================
TestParseRefusesOtherSigner
================
*/
func TestParseRefusesOtherSigner(t *testing.T) {
	_, private := testLicensor(t)
	other, _ := testLicensor(t)
	data, err := Sign(testGrant("opensro.online"), private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data, other); err == nil {
		t.Fatal("a key signed by someone else verified")
	}
}

/*
================
TestParseRefusesEditedGrant

Changing the signed bytes, here to add a domain, breaks the signature.
================
*/
func TestParseRefusesEditedGrant(t *testing.T) {
	public, private := testLicensor(t)
	data, err := Sign(testGrant("opensro.online"), private)
	if err != nil {
		t.Fatal(err)
	}
	var file keyFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	edited := testGrant("opensro.online", "pirate.example")
	edited.Version = keyVersion
	payload, _ := json.Marshal(edited)
	file.Grant = base64.StdEncoding.EncodeToString(payload)
	tampered, _ := json.Marshal(file)
	if _, err := Parse(tampered, public); err == nil {
		t.Fatal("an edited grant verified")
	}
}

/*
================
TestSignRefusesBadGrants
================
*/
func TestSignRefusesBadGrants(t *testing.T) {
	_, private := testLicensor(t)
	long := testGrant("opensro.online")
	long.Expires = long.Issued.Add(MaxLifetime + time.Hour)
	cases := map[string]Grant{
		"too long":    long,
		"no domain":   testGrant(),
		"localhost":   testGrant("localhost"),
		"address":     testGrant("203.0.113.7"),
		"single name": testGrant("intranet"),
		"no operator": {Domains: []string{"opensro.online"}, Issued: testNow, Expires: testNow.Add(time.Hour)},
	}
	for name, grant := range cases {
		if _, err := Sign(grant, private); err == nil {
			t.Errorf("%s: signed", name)
		}
	}
}

/*
================
TestDecide
================
*/
func TestDecide(t *testing.T) {
	official := testGrant("opensro.online")
	cases := []struct {
		name       string
		deployment Deployment
		now        time.Time
		want       Mode
	}{
		{"loopback dev", Deployment{Origins: []string{"http://127.0.0.1:5180", "http://localhost:5180"}}, testNow, ModePrivate},
		{"lan and tailscale", Deployment{Origins: []string{"http://192.168.1.20:5180", "http://100.101.102.103"}}, testNow, ModePrivate},
		{"public no key", Deployment{Origins: []string{"https://play.example"}}, testNow, ModeUnlicensed},
		{"public address no key", Deployment{Origins: []string{"http://203.0.113.7"}}, testNow, ModeUnlicensed},
		{"funnel name is public", Deployment{Origins: []string{"https://sro.tail1234.ts.net"}}, testNow, ModeUnlicensed},
		{"keyed", Deployment{Origins: []string{"https://opensro.online", "https://play.opensro.online"}, Key: &official}, testNow, ModeKeyed},
		{"keyed with local origins", Deployment{Origins: []string{"https://opensro.online", "http://127.0.0.1:5180"}, Key: &official}, testNow, ModeKeyed},
		{"key for another domain", Deployment{Origins: []string{"https://opensro.online", "https://pirate.example"}, Key: &official}, testNow, ModeUnlicensed},
		{"suffix is not a subdomain", Deployment{Origins: []string{"https://notopensro.online"}, Key: &official}, testNow, ModeUnlicensed},
		{"expired", Deployment{Origins: []string{"https://opensro.online"}, Key: &official}, official.Expires, ModeExpired},
		{"not yet valid", Deployment{Origins: []string{"https://opensro.online"}, Key: &official}, testNow.Add(-time.Hour), ModeUnlicensed},
		{"published source", Deployment{Origins: []string{"https://play.example"}, SourceURL: "https://git.example/fork"}, testNow, ModeSource},
		{"source on a private host", Deployment{Origins: []string{"https://play.example"}, SourceURL: "https://127.0.0.1/fork"}, testNow, ModeUnlicensed},
	}
	for _, c := range cases {
		status := Decide(c.deployment, c.now)
		if status.Mode != c.want {
			t.Errorf("%s: mode %s (%s), want %s", c.name, status.Mode, status.Reason, c.want)
		}
		if status.Licensed() != (c.want == ModePrivate || c.want == ModeSource || c.want == ModeKeyed) {
			t.Errorf("%s: Licensed() = %t", c.name, status.Licensed())
		}
	}
}

/*
================
TestLoad
================
*/
func TestLoad(t *testing.T) {
	public, private := testLicensor(t)
	licensor := func() (ed25519.PublicKey, error) { return public, nil }
	data, err := Sign(testGrant("opensro.online"), private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "server-key.json")
	if err := os.WriteFile(good, data, 0o600); err != nil {
		t.Fatal(err)
	}
	origins := []string{"https://opensro.online"}
	deployment, err := Load(Inputs{KeyPath: good}, origins, licensor)
	if err != nil {
		t.Fatal(err)
	}
	if Decide(deployment, testNow).Mode != ModeKeyed {
		t.Fatal("loaded key does not license its domain")
	}
	if _, err := Load(Inputs{KeyPath: filepath.Join(dir, "missing.json")}, origins, licensor); err == nil {
		t.Fatal("a missing key file loaded")
	}
	if _, err := Load(Inputs{SourceURL: "http://git.example/fork"}, origins, licensor); err == nil {
		t.Fatal("a plain-http source link loaded")
	}
}
