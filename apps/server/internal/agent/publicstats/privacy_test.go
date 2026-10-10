/*
===========================================================================

privacy_test.go - the privacy write's guards and its immediate effect

===========================================================================
*/
package publicstats

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testWriteToken is long enough for MinWriteTokenBytes.
const testWriteToken = "0123456789abcdef0123456789abcdef"

/*
================
privacyService

The fixture with a token and an owner-checked setter over its characters.
================
*/
func privacyService(f *fixture, token string) *Service {
	f.chars[0].AccountID = "alpha"
	f.chars[1].AccountID = "beta"
	s := f.service()
	s.src.WriteToken = token
	s.src.SetHidden = func(account, name string, hidden bool) error {
		for _, c := range f.chars {
			if strings.EqualFold(c.Name, name) && c.AccountID == account {
				c.PublicHidden = hidden
				return nil
			}
		}
		return ErrNotOwned
	}
	return s
}

/*
================
put
================
*/
func put(s *Service, path, token, body string) int {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rec, request)
	return rec.Code
}

/*
================
TestPrivacyWriteHidesAtOnce

A cached profile disappears on the next read after the write, not after the
cache period, and comes back when the player shows it again.
================
*/
func TestPrivacyWriteHidesAtOnce(t *testing.T) {
	f := newFixture()
	s := privacyService(f, testWriteToken)
	if code := get(t, s, "/public/v1/characters/Kekw", nil); code != http.StatusOK {
		t.Fatalf("profile before hiding = %d", code)
	}
	if code := put(s, "/v1/accounts/alpha/characters/kekw/hidden", testWriteToken, `{"hidden":true}`); code != http.StatusNoContent {
		t.Fatalf("hide = %d", code)
	}
	if code := get(t, s, "/public/v1/characters/Kekw", nil); code != http.StatusNotFound {
		t.Fatalf("profile after hiding = %d, want 404 at once", code)
	}
	if code := put(s, "/v1/accounts/alpha/characters/Kekw/hidden", testWriteToken, `{"hidden":false}`); code != http.StatusNoContent {
		t.Fatalf("show = %d", code)
	}
	if code := get(t, s, "/public/v1/characters/Kekw", nil); code != http.StatusOK {
		t.Fatalf("profile after showing = %d", code)
	}
}

/*
================
TestPrivacyWriteGuards

Token, body and ownership are each refused; another account's character
answers the same 404 as an unknown name and stays visible.
================
*/
func TestPrivacyWriteGuards(t *testing.T) {
	f := newFixture()
	s := privacyService(f, testWriteToken)
	cases := []struct {
		name, path, token, body string
		want                    int
	}{
		{"no token", "/v1/accounts/alpha/characters/Kekw/hidden", "", `{"hidden":true}`, http.StatusUnauthorized},
		{"wrong token", "/v1/accounts/alpha/characters/Kekw/hidden", strings.Repeat("x", 32), `{"hidden":true}`, http.StatusUnauthorized},
		{"missing field", "/v1/accounts/alpha/characters/Kekw/hidden", testWriteToken, `{}`, http.StatusBadRequest},
		{"unknown field", "/v1/accounts/alpha/characters/Kekw/hidden", testWriteToken, `{"hidden":true,"x":1}`, http.StatusBadRequest},
		{"other account", "/v1/accounts/beta/characters/Kekw/hidden", testWriteToken, `{"hidden":true}`, http.StatusNotFound},
		{"unknown name", "/v1/accounts/alpha/characters/Nobody/hidden", testWriteToken, `{"hidden":true}`, http.StatusNotFound},
	}
	for _, c := range cases {
		if code := put(s, c.path, c.token, c.body); code != c.want {
			t.Errorf("%s = %d, want %d", c.name, code, c.want)
		}
	}
	if f.chars[0].PublicHidden {
		t.Fatal("a refused write changed the character")
	}
}

/*
================
TestPrivacyWriteNeedsAToken

Without a configured token (or with a short one) the route does not exist.
================
*/
func TestPrivacyWriteNeedsAToken(t *testing.T) {
	for _, token := range []string{"", "short"} {
		f := newFixture()
		s := privacyService(f, token)
		if code := put(s, "/v1/accounts/alpha/characters/Kekw/hidden", token, `{"hidden":true}`); code != http.StatusNotFound {
			t.Errorf("token %q: write = %d, want 404 (not served)", token, code)
		}
	}
}
