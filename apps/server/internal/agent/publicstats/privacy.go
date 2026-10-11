/*
===========================================================================

privacy.go - the website's privacy setting (port-only)

	GET /v1/accounts/{id}/characters                 -> 200 own characters
	PUT /v1/accounts/{id}/characters/{name}/hidden  {"hidden": bool} -> 204

The /account page lists the signed-in player's own characters with their
current flag and sets it. Both are served on the same loopback listener as
the reads but only when a write token is configured, and every request
carries that bearer token: the
listener keeps it off the network, the token keeps other local processes
out (the provisioning API's two guards). The character must belong to the
account in the path; any other name answers 404, exactly like an unknown
one, so the write cannot be used to probe who owns what.

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

// ErrNotOwned answers 404: no such character on that account.
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
	mux.HandleFunc("GET /v1/accounts/{id}/characters", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		s.ownCharacters(w, r.PathValue("id"))
	})
	mux.HandleFunc("PUT /v1/accounts/{id}/characters/{name}/hidden", func(w http.ResponseWriter, r *http.Request) {
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
		if err := s.src.SetHidden(account, name, *body.Hidden); err != nil {
			if errors.Is(err, ErrNotOwned) {
				writeStatus(w, http.StatusNotFound, "not found")
				return
			}
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		s.mu.Lock()
		s.cache = map[string]cacheEntry{}
		// The roster holds the flag; the kill list does not and stays.
		s.snap.roster, s.snap.rosterUntil, s.snap.rankings = roster{}, time.Time{}, nil
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
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
		out.Characters = append(out.Characters, OwnCharacter{Name: c.Name, Level: level(c), Race: race, Hidden: c.PublicHidden})
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
