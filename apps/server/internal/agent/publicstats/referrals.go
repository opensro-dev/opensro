/*
===========================================================================

referrals.go - the website's referral routes (#3518, port-only)

	GET /v1/accounts/{id}/referrals          -> 200 own code, rules, invites
	PUT /v1/accounts/{id}/referrer  {"code","held"} -> 204
	GET /v1/referral-codes/{code}            -> 200 {"inviter","account"}, 404
	GET /v1/referrals/held                   -> 200 referrals waiting for staff
	PUT /v1/referrals/{referee}/approved     -> 204

Port-only, not native (SRO_REFERRALS): served only when the flag wired a
ReferralSource and the write token is configured, behind that bearer token
like the privacy writes. Off, every route answers 404.

Privacy: a player's own list names no invited account and no character of
one, only when each joined and how far it got. The code lookup the sign-up
page shows names the inviter by their highest character that is not
hidden, or nothing: the inviter handed the code out, and the page only
says whose invitation it is. The lookup also carries the inviter's account
id for the website's own use (comparing the two sign-ins' connections); the
site never shows it. The held list is for staff (the website's
admin page) and carries account ids.

The website decides "held": it sees both sign-ins' connections, the game
does not. The rules (qualifying level, rewards, cap) are the server's
constants, served so the site never writes its own numbers.

===========================================================================
*/
package publicstats

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"

	"opensro.online/server/internal/domain"
)

const (
	// maxReferrerBody bounds the link request body.
	maxReferrerBody = 256
	// heldReferralsMax caps the staff list.
	heldReferralsMax = 200
)

/*
================
ReferralSource

The store's referral doors (data/store/referrals.go).
================
*/
type ReferralSource interface {
	ReferralCode(accountID string) (string, error)
	ReferralCodeOwner(code string) (string, error)
	LinkReferral(referee, code string, held bool) error
	ReferralsBy(referrer string) ([]domain.Referral, error)
	HeldReferrals(limit int) ([]domain.Referral, error)
	ApproveReferral(referee string) error
}

/*
================
ReferralRules

The server's referral constants, as the site shows them.
================
*/
type ReferralRules struct {
	Level        int64  `json:"level"`
	ReferrerSilk uint32 `json:"referrerSilk"`
	RefereeSilk  uint32 `json:"refereeSilk"`
	RewardCap    int    `json:"rewardCap"`
}

// The invite states the site shows.
const (
	referralHeld      = "held"
	referralJoined    = "joined"
	referralQualified = "qualified"
)

// ReferralInvite is one invited account as its inviter sees it: when, and
// how far, never who.
type ReferralInvite struct {
	LinkedMs    int64  `json:"linkedMs"`
	Status      string `json:"status"`
	QualifiedMs int64  `json:"qualifiedMs,omitempty"`
	EarnedSilk  uint32 `json:"earnedSilk"`
}

// OwnReferralsResponse answers GET /v1/accounts/{id}/referrals.
type OwnReferralsResponse struct {
	Code       string           `json:"code"`
	Rules      ReferralRules    `json:"rules"`
	Invites    []ReferralInvite `json:"invites"`
	EarnedSilk uint32           `json:"earnedSilk"`
}

// ReferralCodeResponse answers GET /v1/referral-codes/{code}.
type ReferralCodeResponse struct {
	Inviter string `json:"inviter"`
	Account string `json:"account"`
}

// HeldReferralsResponse answers GET /v1/referrals/held.
type HeldReferralsResponse struct {
	Referrals []HeldReferral `json:"referrals"`
}

// HeldReferral is one referral waiting for staff.
type HeldReferral struct {
	Referee  string `json:"referee"`
	Referrer string `json:"referrer"`
	LinkedMs int64  `json:"linkedMs"`
}

/*
================
installReferrals

Adds the referral routes when SRO_REFERRALS wired a source and the write
token is configured.
================
*/
func (s *Service) installReferrals(mux *http.ServeMux, digest [sha256.Size]byte) {
	if s.src.Referrals == nil {
		return
	}
	guarded := func(handle func(w http.ResponseWriter, r *http.Request)) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			if authorized(w, r, digest) {
				handle(w, r)
			}
		}
	}
	mux.HandleFunc("GET /v1/accounts/{id}/referrals", guarded(s.ownReferrals))
	mux.HandleFunc("PUT /v1/accounts/{id}/referrer", guarded(s.putReferrer))
	mux.HandleFunc("GET /v1/referral-codes/{code}", guarded(s.referralCode))
	mux.HandleFunc("GET /v1/referrals/held", guarded(s.heldReferrals))
	mux.HandleFunc("PUT /v1/referrals/{referee}/approved", guarded(s.approveReferral))
}

/*
================
ownReferrals
================
*/
func (s *Service) ownReferrals(w http.ResponseWriter, r *http.Request) {
	account := r.PathValue("id")
	code, err := s.src.Referrals.ReferralCode(account)
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	rows, err := s.src.Referrals.ReferralsBy(account)
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	out := OwnReferralsResponse{Code: code, Rules: s.src.ReferralRules, Invites: []ReferralInvite{}}
	for _, row := range rows {
		invite := ReferralInvite{LinkedMs: row.LinkedMs, Status: referralJoined, EarnedSilk: row.ReferrerSilk}
		switch {
		case row.QualifiedMs != 0:
			invite.Status, invite.QualifiedMs = referralQualified, row.QualifiedMs
		case row.Held:
			invite.Status = referralHeld
		}
		out.EarnedSilk += row.ReferrerSilk
		out.Invites = append(out.Invites, invite)
	}
	writeJSON(w, out)
}

/*
================
putReferrer

Links the account to the code's owner: 204; 404 for an unknown code; 409
when it already names one; 422 for its own code or an account that already
plays.
================
*/
func (s *Service) putReferrer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		Held bool   `json:"held"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReferrerBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Code == "" {
		writeStatus(w, http.StatusBadRequest, "body must be {\"code\": string, \"held\": bool}")
		return
	}
	err := s.src.Referrals.LinkReferral(r.PathValue("id"), body.Code, body.Held)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, domain.ErrReferralUnknownCode):
		writeStatus(w, http.StatusNotFound, "unknown code")
	case errors.Is(err, domain.ErrReferralLinked):
		writeStatus(w, http.StatusConflict, "already linked")
	case errors.Is(err, domain.ErrReferralSelf), errors.Is(err, domain.ErrReferralTooLate):
		writeStatus(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
	}
}

/*
================
referralCode

Whose code it is, by the inviter's highest character that is not hidden;
empty when every one is hidden or there is none yet.
================
*/
func (s *Service) referralCode(w http.ResponseWriter, r *http.Request) {
	owner, err := s.src.Referrals.ReferralCodeOwner(r.PathValue("code"))
	if errors.Is(err, domain.ErrReferralUnknownCode) {
		writeStatus(w, http.StatusNotFound, "unknown code")
		return
	}
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	inviter, best := "", int64(-1)
	s.mu.Lock()
	for _, c := range s.loadRoster().all {
		if c.AccountID != owner || c.PublicHidden {
			continue
		}
		if lvl := level(c); lvl > best {
			inviter, best = c.Name, lvl
		}
	}
	s.mu.Unlock()
	writeJSON(w, ReferralCodeResponse{Inviter: inviter, Account: owner})
}

/*
================
heldReferrals
================
*/
func (s *Service) heldReferrals(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.src.Referrals.HeldReferrals(heldReferralsMax)
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	out := HeldReferralsResponse{Referrals: []HeldReferral{}}
	for _, row := range rows {
		out.Referrals = append(out.Referrals, HeldReferral{Referee: row.Referee, Referrer: row.Referrer, LinkedMs: row.LinkedMs})
	}
	writeJSON(w, out)
}

/*
================
approveReferral
================
*/
func (s *Service) approveReferral(w http.ResponseWriter, r *http.Request) {
	err := s.src.Referrals.ApproveReferral(r.PathValue("referee"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, domain.ErrReferralNotFound):
		writeStatus(w, http.StatusNotFound, "not found")
	default:
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
	}
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "encode")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
