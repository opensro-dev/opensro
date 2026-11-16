/*
===========================================================================

public_contributor_test.go - the Contributor title's read and take through the public API

Port-only, not native (#135). The website reaches the registry only through
publicSources, so these tests drive the real handler: with the title off
every authenticated call answers 404; with it on, a contributor reads
whether its own account carries the title and moves it onto that account
only, and a save the store refuses answers 503 with the old choice kept.

===========================================================================
*/
package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/contributor"
)

// titleToken is the privacy token these tests send; it is exactly the
// publicstats minimum, so the write is served. A const, so the package
// holds no mutable state.
const titleToken = "0123456789abcdef0123456789abcdef"

// titleMembers is the sync's roster: member m1 links a1 (oldest) and a2;
// member m2 links b1.
const titleMembers = `{"members":[{"member":"m1","accounts":["a1","a2"]},{"member":"m2","accounts":["b1"]}],"accounts":["a1","b1"]}`

/*
================
titleAPI

The public API's handler over sources built the way the GameWorld builds
them, for a game plane whose registry the flag decides (nil when off). The
write token is read from a file, as production reads it.
================
*/
func titleAPI(t *testing.T, game *gameplayPlane) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "public-api-token")
	if err := os.WriteFile(path, []byte(titleToken), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(publicstats.EnvTokenPath, path)
	return publicstats.New(publicSources(game, nil, nil, "test", nil, nil)).Handler()
}

/*
================
titleCall

One request through the handler: its status and body. An empty token sends
no Authorization header.
================
*/
func titleCall(handler http.Handler, method, path, token, body string) (int, string) {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(rec, request)
	return rec.Code, rec.Body.String()
}

/*
================
titleRead

The account's Contributor title as its page reads it: the status and, on
200, the decoded answer.
================
*/
func titleRead(t *testing.T, handler http.Handler, account string) (int, publicstats.ContributorTitleResponse) {
	t.Helper()
	code, body := titleCall(handler, http.MethodGet, "/v1/accounts/"+account+"/contributor-title", titleToken, "")
	var out publicstats.ContributorTitleResponse
	if code == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
	}
	return code, out
}

/*
================
titleTake

The page's "carry it here" by account: the status it answers.
================
*/
func titleTake(handler http.Handler, account string) int {
	code, _ := titleCall(handler, http.MethodPut, "/v1/accounts/"+account+"/contributor-title", titleToken, "")
	return code
}

/*
================
TestContributorTitleAnswersNotFoundWithTheFlagOff

With SRO_CONTRIBUTOR_TITLES off there is no registry: every authenticated
call to the title answers 404, and a call without the token is still
refused by the token guard.
================
*/
func TestContributorTitleAnswersNotFoundWithTheFlagOff(t *testing.T) {
	handler := titleAPI(t, &gameplayPlane{deps: &enterworld.Deps{}})
	if code, _ := titleRead(t, handler, "a1"); code != http.StatusNotFound {
		t.Fatalf("read with the title off = %d, want 404", code)
	}
	if code := titleTake(handler, "a2"); code != http.StatusNotFound {
		t.Fatalf("take with the title off = %d, want 404", code)
	}
	if code, _ := titleCall(handler, http.MethodGet, "/v1/accounts/a1/contributor-title", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("read without a token = %d, want 401", code)
	}
}

/*
================
TestContributorTitleTakeMovesItOntoTheSignedInAccount

With the flag on, each of a member's accounts reads only whether it
carries the title; no answer names the member's other accounts. A take
moves the title onto the signed-in account (204) and both accounts' next
reads show it; another member keeps its own. An account that is no
contributor answers 404. The hidden flag follows the account's own
"/contributor off". A save the store refuses answers 503 and keeps the
holder where it was.
================
*/
func TestContributorTitleTakeMovesItOntoTheSignedInAccount(t *testing.T) {
	dir := t.TempDir()
	members := filepath.Join(dir, contributor.MembersFileName)
	if err := os.WriteFile(members, []byte(titleMembers), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := contributor.Open(dir, members, func(path string, body []byte) error { return os.WriteFile(path, body, 0o600) })
	if err != nil {
		t.Fatal(err)
	}
	handler := titleAPI(t, &gameplayPlane{deps: &enterworld.Deps{}, contributors: registry})

	code, body := titleCall(handler, http.MethodGet, "/v1/accounts/a2/contributor-title", titleToken, "")
	if code != http.StatusOK || strings.Contains(body, "a1") {
		t.Fatalf("a2's read = %d %s, want 200 naming no other account", code, body)
	}
	if _, out := titleRead(t, handler, "a1"); !out.Carries || out.Hidden {
		t.Fatalf("a1's read = %+v, want the holder", out)
	}
	if code := titleTake(handler, "c1"); code != http.StatusNotFound {
		t.Fatalf("take by a non-contributor = %d, want 404", code)
	}
	if code := titleTake(handler, "a2"); code != http.StatusNoContent {
		t.Fatalf("take = %d, want 204", code)
	}
	if _, out := titleRead(t, handler, "a2"); !out.Carries {
		t.Fatalf("a2 after the take = %+v, want the holder", out)
	}
	if _, out := titleRead(t, handler, "a1"); out.Carries {
		t.Fatalf("a1 after a2's take = %+v, want it moved away", out)
	}
	if _, out := titleRead(t, handler, "b1"); !out.Carries {
		t.Fatalf("another member lost its title: %+v", out)
	}
	registry.Command("a2", "/contributor off")
	if _, out := titleRead(t, handler, "a2"); !out.Hidden {
		t.Fatalf("a2's hidden flag not read: %+v", out)
	}

	// A store that refuses the save: the take answers 503 and the title stays on a1.
	refusedDir := t.TempDir()
	refusedMembers := filepath.Join(refusedDir, contributor.MembersFileName)
	if err := os.WriteFile(refusedMembers, []byte(titleMembers), 0o600); err != nil {
		t.Fatal(err)
	}
	refused, err := contributor.Open(refusedDir, refusedMembers, func(string, []byte) error { return errors.New("disk full") })
	if err != nil {
		t.Fatal(err)
	}
	refusedHandler := titleAPI(t, &gameplayPlane{deps: &enterworld.Deps{}, contributors: refused})
	if code := titleTake(refusedHandler, "a2"); code != http.StatusServiceUnavailable {
		t.Fatalf("refused save = %d, want 503", code)
	}
	if _, out := titleRead(t, refusedHandler, "a1"); !out.Carries {
		t.Fatalf("a refused save moved the title: %+v", out)
	}
}
