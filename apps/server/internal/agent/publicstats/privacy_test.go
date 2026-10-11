/*
===========================================================================

privacy_test.go - the privacy write's guards and its immediate effect

===========================================================================
*/
package publicstats

import (
	"encoding/json"
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
	s.src.SetGearHidden = func(account, name string, hidden bool) error {
		for _, c := range f.chars {
			if strings.EqualFold(c.Name, name) && c.AccountID == account {
				c.PublicGearHidden = hidden
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

/*
================
authorizedGet
================
*/
func authorizedGet(s *Service, path, token string) (int, []byte) {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rec, request)
	return rec.Code, rec.Body.Bytes()
}

/*
================
TestOwnCharactersListTheAccountWithItsFlags

The account's own characters, hidden ones included with their flag, and
nobody else's; a privacy write shows in the next list at once; the token
guards it and no token leaves it unserved.
================
*/
func TestOwnCharactersListTheAccountWithItsFlags(t *testing.T) {
	f := newFixture()
	f.chars[2].AccountID = "alpha"
	s := privacyService(f, testWriteToken)
	list := func(account string) OwnCharactersResponse {
		t.Helper()
		code, body := authorizedGet(s, "/v1/accounts/"+account+"/characters", testWriteToken)
		if code != http.StatusOK {
			t.Fatalf("list %s answered %d", account, code)
		}
		var out OwnCharactersResponse
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	alpha := list("alpha")
	if len(alpha.Characters) != 2 || alpha.Characters[0].Name != "Kekw" || alpha.Characters[0].Hidden ||
		alpha.Characters[1].Name != "Shy" || !alpha.Characters[1].Hidden || alpha.Characters[0].Race != "chinese" {
		t.Fatalf("alpha lists %+v, want Kekw shown and Shy hidden", alpha.Characters)
	}
	if beta := list("beta"); len(beta.Characters) != 1 || beta.Characters[0].Name != "Lune" {
		t.Fatalf("beta lists %+v", beta.Characters)
	}
	if nobody := list("nobody"); nobody.Characters == nil || len(nobody.Characters) != 0 {
		t.Fatalf("an unknown account lists %+v, want an empty array", nobody.Characters)
	}
	if code := put(s, "/v1/accounts/alpha/characters/Kekw/hidden", testWriteToken, `{"hidden":true}`); code != http.StatusNoContent {
		t.Fatalf("hide = %d", code)
	}
	if after := list("alpha"); !after.Characters[0].Hidden {
		t.Fatalf("the list did not show the write at once: %+v", after.Characters)
	}
	if code, _ := authorizedGet(s, "/v1/accounts/alpha/characters", strings.Repeat("x", 32)); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", code)
	}
	unserved := privacyService(newFixture(), "")
	if code, _ := authorizedGet(unserved, "/v1/accounts/alpha/characters", ""); code != http.StatusNotFound {
		t.Fatalf("no token configured = %d, want 404", code)
	}
}

/*
================
TestGearPrivacyHidesTheRollsAtOnce

"Hide my gear" keeps the profile and its items (the 3D view draws them)
but drops each item's rolls, blue stats and durability on the next read;
the owner's list shows the choice; another account cannot set it.
================
*/
func TestGearPrivacyHidesTheRollsAtOnce(t *testing.T) {
	f := newFixture()
	s := privacyService(f, testWriteToken)
	var before Profile
	get(t, s, "/public/v1/characters/Kekw", &before)
	if before.GearHidden || before.Look.Worn[0].Variance == "" || len(before.Look.Worn[0].MagicOptions) == 0 {
		t.Fatalf("profile before hiding gear %+v", before)
	}
	if code := put(s, "/v1/accounts/beta/characters/kekw/gear-hidden", testWriteToken, `{"hidden":true}`); code != http.StatusNotFound {
		t.Fatalf("another account's write = %d, want 404", code)
	}
	if code := put(s, "/v1/accounts/alpha/characters/kekw/gear-hidden", testWriteToken, `{"hidden":true}`); code != http.StatusNoContent {
		t.Fatalf("hide gear = %d", code)
	}
	var after Profile
	if code := get(t, s, "/public/v1/characters/Kekw", &after); code != http.StatusOK {
		t.Fatalf("profile after hiding gear = %d, want it still shown", code)
	}
	w := after.Look.Worn[0]
	if !after.GearHidden || w.RefItemID != 3801 || w.Plus != 7 || w.Variance != "" || w.Durability != nil || w.MagicOptions != nil {
		t.Fatalf("profile after hiding gear %+v", after)
	}
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/accounts/alpha/characters", nil)
	request.Header.Set("Authorization", "Bearer "+testWriteToken)
	s.Handler().ServeHTTP(rec, request)
	var own OwnCharactersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &own); err != nil || len(own.Characters) != 1 || !own.Characters[0].GearHidden {
		t.Fatalf("own characters %s %v", rec.Body.String(), err)
	}
	if code := put(s, "/v1/accounts/alpha/characters/Kekw/gear-hidden", "", `{"hidden":false}`); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated write = %d", code)
	}
}
