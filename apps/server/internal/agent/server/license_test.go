/*
===========================================================================

license_test.go - the Agent's license admission and reports

===========================================================================
*/
package agentserver

import (
	"testing"
	"time"

	"opensro.online/server/internal/security/serverkey"
)

/*
================
TestAdmitLicense
================
*/
func TestAdmitLicense(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	grant := serverkey.Grant{Operator: "official", Domains: []string{"opensro.online"}, Issued: now, Expires: now.Add(serverkey.DefaultLifetime)}
	cases := []struct {
		name    string
		license serverkey.Deployment
		origins []string
		admit   bool
	}{
		{"loopback", serverkey.Deployment{}, []string{"http://127.0.0.1:5180"}, true},
		{"public without key", serverkey.Deployment{}, []string{"https://play.example"}, false},
		{"public with key", serverkey.Deployment{Key: &grant}, []string{"https://opensro.online"}, true},
		{"key for another domain", serverkey.Deployment{Key: &grant}, []string{"https://play.example"}, false},
		{"published source", serverkey.Deployment{SourceURL: "https://git.example/fork"}, []string{"https://play.example"}, true},
		// The configured origins decide, whatever the caller put in Origins.
		{"origins come from the Agent", serverkey.Deployment{Origins: []string{"http://127.0.0.1"}}, []string{"https://play.example"}, false},
	}
	for _, c := range cases {
		_, err := admitLicense(c.license, c.origins, now)
		if (err == nil) != c.admit {
			t.Errorf("%s: admitted %t, want %t (%v)", c.name, err == nil, c.admit, err)
		}
	}
	if _, err := admitLicense(serverkey.Deployment{Key: &grant}, []string{"https://opensro.online"}, grant.Expires); err == nil {
		t.Error("an expired key admitted a start")
	}
}

/*
================
TestLicenseLapsesWhileRunning

A key that expires after start leaves the Agent serving but reports the
lapse and closes the mall.
================
*/
func TestLicenseLapsesWhileRunning(t *testing.T) {
	clock := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	grant := serverkey.Grant{Operator: "official", Domains: []string{"opensro.online"}, Issued: clock, Expires: clock.Add(time.Hour)}
	license, err := admitLicense(serverkey.Deployment{Key: &grant}, []string{"https://opensro.online"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{license: license, now: func() time.Time { return clock }}
	if !server.licenseStatus().Licensed() || server.licenseProjection()["mode"] != "keyed" {
		t.Fatalf("fresh key: %+v", server.licenseProjection())
	}
	clock = clock.Add(2 * time.Hour)
	if server.licenseStatus().Licensed() || server.licenseProjection()["mode"] != "expired" {
		t.Fatalf("lapsed key: %+v", server.licenseProjection())
	}
}
