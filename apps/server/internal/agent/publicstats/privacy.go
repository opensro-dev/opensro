/*
===========================================================================

privacy.go - the website's privacy setting (port-only)

	GET /v1/accounts/{id}/characters                 -> 200 own characters
	GET /v1/accounts/{id}/trades                     -> 200 own trade history (trades.go)
	PUT /v1/accounts/{id}/characters/{name}/hidden       {"hidden": bool} -> 204
	PUT /v1/accounts/{id}/characters/{name}/gear-hidden  {"hidden": bool} -> 204
	PUT /v1/accounts/{id}/characters/{name}/break-notice {"on": bool} -> 204
	GET /v1/accounts/{id}/contributor-title              -> 200 this account's title (#135)
	PUT /v1/accounts/{id}/contributor-title              -> 204 the title moves here

The /account page lists the signed-in player's own characters with their
current flag and sets it. Both are served on the same loopback listener as
the reads but only when a write token is configured, and every request
carries that bearer token: the
listener keeps it off the network, the token keeps other local processes
out (the provisioning API's two guards). The character must belong to the
account in the path; any other name answers 404, exactly like an unknown
one, so the write cannot be used to probe who owns what.

The Contributor title (port-only, not native; #135) rides the same guards.
The read says only whether the signed-in account carries it, and the write
only moves it onto the signed-in account (contributor.Registry.TakeTitle,
as "/contributor here" in game). A contributor's group holds only the
accounts the member linked by their own Discord sign-in (the operations
sync drops invite-key links, opensro-ops#376), and still no answer names
another account. An account that is no contributor, an invite-made one
among them, answers 404.

A successful write empties the answer cache and the shared snapshot: a
player who hides is gone from every read at once, not after the cache
period.

===========================================================================
*/
package publicstats

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// EnvTokenPath names the file holding the write token (the provisioning
// token the website already holds). Unset or empty disables the write.
const EnvTokenPath = "SRO_PUBLIC_API_TOKEN_PATH"

// MinWriteTokenBytes refuses a guessable token: a shorter one leaves the
// write unserved, so a bad secret render fails closed.
const MinWriteTokenBytes = 32

// maxPrivacyBody bounds the request body.
const maxPrivacyBody = 256

// ErrNotOwned answers 404: no such character on that account, and, for the
// Contributor title's pick, no such account in the signed-in one's group.
var ErrNotOwned = errors.New("character not found on this account")

/*
================
installPrivacy

Adds the write route when the token and the setter are both present.
================
*/
func (s *Service) installPrivacy(mux *http.ServeMux) {
	token := strings.TrimSpace(s.src.WriteToken)
	if len(token) < MinWriteTokenBytes || s.src.SetHidden == nil {
		return
	}
	digest := sha256.Sum256([]byte(token))
	s.installMarket(mux, digest)
	s.installReferrals(mux, digest)
	mux.HandleFunc("GET /v1/accounts/{id}/characters", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		s.ownCharacters(w, r.PathValue("id"))
	})
	if s.src.Trades != nil {
		s.installTrades(mux, digest)
	}
	mux.HandleFunc("PUT /v1/accounts/{id}/characters/{name}/hidden", func(w http.ResponseWriter, r *http.Request) {
		s.putFlag(w, r, digest, s.src.SetHidden)
	})
	if s.src.SetGearHidden != nil {
		mux.HandleFunc("PUT /v1/accounts/{id}/characters/{name}/gear-hidden", func(w http.ResponseWriter, r *http.Request) {
			s.putFlag(w, r, digest, s.src.SetGearHidden)
		})
	}
	if s.src.SetBreakNotice != nil {
		mux.HandleFunc("PUT /v1/accounts/{id}/characters/{name}/break-notice", func(w http.ResponseWriter, r *http.Request) {
			s.putBreakNotice(w, r, digest)
		})
	}
	// The Contributor title's read and pick (port-only, not native; #135)
	// sit behind the same token. The game sets both closures together.
	if s.src.ContributorTitleOf != nil && s.src.TakeContributorTitle != nil {
		mux.HandleFunc("GET /v1/accounts/{id}/contributor-title", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r, digest) {
				return
			}
			s.contributorTitle(w, r.PathValue("id"))
		})
		mux.HandleFunc("PUT /v1/accounts/{id}/contributor-title", func(w http.ResponseWriter, r *http.Request) {
			s.putContributorTitle(w, r, digest)
		})
	}
}

/*
================
putFlag

One privacy write, {"hidden": bool}, through set: 204, 404 for a character
the account does not own (as for an unknown one), 503 when the store did
not take it. A success drops the cached answers and the roster, so every
read shows the change at once.
================
*/
func (s *Service) putFlag(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte, set func(accountID, name string, hidden bool) error) {
	if !authorized(w, r, digest) {
		return
	}
	var body struct {
		Hidden *bool `json:"hidden"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrivacyBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Hidden == nil {
		writeStatus(w, http.StatusBadRequest, "body must be {\"hidden\": true|false}")
		return
	}
	account, name := r.PathValue("id"), r.PathValue("name")
	if account == "" || name == "" || len(name) > maxNameLength {
		writeStatus(w, http.StatusNotFound, "not found")
		return
	}
	if err := set(account, name, *body.Hidden); err != nil {
		if errors.Is(err, ErrNotOwned) {
			writeStatus(w, http.StatusNotFound, "not found")
			return
		}
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	s.mu.Lock()
	s.cache = map[string]cacheEntry{}
	// The roster and the guild list hold the flags; the kill list does not and stays.
	s.snap.roster, s.snap.rosterUntil, s.snap.rankings = roster{}, time.Time{}, nil
	s.snap.guilds, s.snap.guildsUntil = nil, time.Time{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

/*
================
putBreakNotice

The item-break notice opt-in (#1738), {"on": bool}, through
SetBreakNotice: the same contract as putFlag (204, 404 for a character
the account does not own, 503 when the store did not take it), and the
same cache drop, so the character list shows the change at once.
================
*/
func (s *Service) putBreakNotice(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte) {
	if !authorized(w, r, digest) {
		return
	}
	var body struct {
		On *bool `json:"on"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrivacyBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.On == nil {
		writeStatus(w, http.StatusBadRequest, "body must be {\"on\": true|false}")
		return
	}
	account, name := r.PathValue("id"), r.PathValue("name")
	if account == "" || name == "" || len(name) > maxNameLength {
		writeStatus(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.src.SetBreakNotice(account, name, *body.On); err != nil {
		if errors.Is(err, ErrNotOwned) {
			writeStatus(w, http.StatusNotFound, "not found")
			return
		}
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	s.mu.Lock()
	s.cache = map[string]cacheEntry{}
	s.snap.roster, s.snap.rosterUntil, s.snap.rankings = roster{}, time.Time{}, nil
	s.snap.guilds, s.snap.guildsUntil = nil, time.Time{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

/*
================
contributorTitle

The Contributor title the signed-in account sees (port-only, not native;
#135): whether it carries the title and whether it hides it. An account
that is no contributor answers 404, the same answer the write gives.
================
*/
func (s *Service) contributorTitle(w http.ResponseWriter, account string) {
	carries, hidden, ok := s.src.ContributorTitleOf(account)
	if !ok {
		writeStatus(w, http.StatusNotFound, "not found")
		return
	}
	body, err := json.Marshal(ContributorTitleResponse{Carries: carries, Hidden: hidden})
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "encode")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

/*
================
putContributorTitle

The website's "carry it here" (port-only, not native; #135): the signed-in
account's member's title moves onto that account (TakeContributorTitle),
204. The request has no body: there is no other account to name. An
account that is no contributor answers 404; a save the store does not take
answers 503, and the old choice stays in force.
================
*/
func (s *Service) putContributorTitle(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte) {
	if !authorized(w, r, digest) {
		return
	}
	account := r.PathValue("id")
	if account == "" {
		writeStatus(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.src.TakeContributorTitle(account); err != nil {
		if errors.Is(err, ErrNotOwned) {
			writeStatus(w, http.StatusNotFound, "not found")
			return
		}
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
================
authorized

Sets the private headers and checks the bearer token, comparing SHA-256
digests so the time does not depend on the presented token's length.
Answers 401 itself when the token is wrong.
================
*/
func authorized(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	presentedDigest := sha256.Sum256([]byte(presented))
	if !found || subtle.ConstantTimeCompare(presentedDigest[:], digest[:]) != 1 {
		writeStatus(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

/*
================
ownCharacters

The account's live characters from the shared roster, by name. A privacy
write drops the roster, so the list shows a change at once. An unknown
account is an empty list: the site only asks for its signed-in player.
================
*/
func (s *Service) ownCharacters(w http.ResponseWriter, account string) {
	out := OwnCharactersResponse{Characters: []OwnCharacter{}}
	s.mu.Lock()
	r := s.loadRoster()
	for _, c := range r.all {
		if account == "" || c.AccountID != account {
			continue
		}
		race, _ := raceGender(c.ModelCodename)
		out.Characters = append(out.Characters, OwnCharacter{Name: c.Name, Level: level(c), Race: race, Hidden: c.PublicHidden, GearHidden: c.PublicGearHidden, BreakNotice: c.BreakNotice})
	}
	s.mu.Unlock()
	sort.Slice(out.Characters, func(i, j int) bool { return out.Characters[i].Name < out.Characters[j].Name })
	body, err := json.Marshal(out)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "encode")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

/*
================
writeStatus
================
*/
func writeStatus(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]string{"error": message})
	_, _ = w.Write(body)
}
