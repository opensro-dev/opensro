/*
===========================================================================

revocation.go - GameWorld account session and ticket revocation enforcement

Tracks the minimum accepted session epoch per account on this shard, revokes
any unconsumed transport-admission (STA1) or EnterWorld (SEA3) tickets minted
before a revocation, and evicts live transport sessions when the Agent
publishes a signed account revocation notice.

===========================================================================
*/
package agentapi

import (
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/security/auth"
)

const (
	AccountRevokePath = "/internal/control/account-revoke"
	revokeBodyLimit   = 2048
	revokeReplayLimit = 1024
)

/*
================
accountRevoker
================
*/
type accountRevoker struct {
	mu             sync.RWMutex
	minEpoch       map[string]uint64
	revokedGen     map[string]uint64
	issuedTickets  map[string]map[[sha256.Size]byte]int64
	revokedTickets map[[sha256.Size]byte]int64
	seenNotices    map[string]int64
	evictAccount   func(accountID string)
}

/*
================
newAccountRevoker
================
*/
func newAccountRevoker() accountRevoker {
	return accountRevoker{
		minEpoch:       make(map[string]uint64),
		revokedGen:     make(map[string]uint64),
		issuedTickets:  make(map[string]map[[sha256.Size]byte]int64),
		revokedTickets: make(map[[sha256.Size]byte]int64),
		seenNotices:    make(map[string]int64),
	}
}

/*
================
InstallAccountRevoker

Installs the callback that evicts live transport sessions for an account
when its sessions are revoked.
================
*/
func (api *API) InstallAccountRevoker(evict func(accountID string)) {
	api.revocations.mu.Lock()
	api.revocations.evictAccount = evict
	api.revocations.mu.Unlock()
}

/*
================
RevokeAccount

Advances the minimum accepted session epoch for accountID on this shard,
invalidates all unconsumed STA1 and SEA3 tickets minted for accountID prior
to this revocation, and evicts any live transport sessions owned by accountID.
================
*/
func (api *API) RevokeAccount(accountID string, epoch uint64) {
	if epoch < 1 {
		epoch = 2
	}
	nowUnix := api.now().Unix()

	api.revocations.mu.Lock()
	if epoch > api.revocations.minEpoch[accountID] {
		api.revocations.minEpoch[accountID] = epoch
	}
	api.revocations.revokedGen[accountID]++
	for digest, exp := range api.revocations.revokedTickets {
		if exp <= nowUnix {
			delete(api.revocations.revokedTickets, digest)
		}
	}
	for digest, exp := range api.revocations.issuedTickets[accountID] {
		if exp > nowUnix {
			api.revocations.revokedTickets[digest] = exp
		}
	}
	delete(api.revocations.issuedTickets, accountID)
	evict := api.revocations.evictAccount
	api.revocations.mu.Unlock()

	if evict != nil {
		evict(accountID)
	}
}

/*
================
sessionAllowed
================
*/
func (api *API) sessionAllowed(claims auth.AgentSessionClaims) bool {
	api.revocations.mu.RLock()
	minEpoch := api.revocations.minEpoch[claims.AccountID]
	gen := api.revocations.revokedGen[claims.AccountID]
	api.revocations.mu.RUnlock()

	if claims.Epoch == 0 {
		return gen == 0 && minEpoch == 0
	}
	return claims.Epoch >= minEpoch
}

/*
================
recordIssuedTicket
================
*/
func (api *API) recordIssuedTicket(accountID, token string, expiresAt time.Time) {
	if accountID == "" || token == "" {
		return
	}
	digest := sha256.Sum256([]byte(token))
	nowUnix := api.now().Unix()

	api.revocations.mu.Lock()
	tickets := api.revocations.issuedTickets[accountID]
	if tickets == nil {
		tickets = make(map[[sha256.Size]byte]int64)
		api.revocations.issuedTickets[accountID] = tickets
	} else {
		for prior, exp := range tickets {
			if exp <= nowUnix {
				delete(tickets, prior)
			}
		}
	}
	tickets[digest] = expiresAt.Unix()
	api.revocations.mu.Unlock()
}

/*
================
TicketRevoked

Reports whether an unconsumed STA1 or SEA3 ticket was invalidated by an
account revocation after it was minted.
================
*/
func (api *API) TicketRevoked(token string) bool {
	if token == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	nowUnix := api.now().Unix()

	api.revocations.mu.RLock()
	exp, revoked := api.revocations.revokedTickets[digest]
	api.revocations.mu.RUnlock()
	return revoked && exp > nowUnix
}

/*
================
WrapTransportAdmissionVerifier

Decorates a STA1 transport-admission verifier so tickets minted before an
account revocation are refused.
================
*/
func (api *API) WrapTransportAdmissionVerifier(
	verify auth.TransportAdmissionVerifyFunc,
) auth.TransportAdmissionVerifyFunc {
	return func(token string) (auth.TransportAdmissionClaims, error) {
		if api.TicketRevoked(token) {
			return auth.TransportAdmissionClaims{}, auth.ErrTransportAdmissionExpired
		}
		return verify(token)
	}
}

/*
================
WrapEnterWorldVerifier

Decorates a SEA3 EnterWorld verifier so bind tickets minted before an
account revocation are refused.
================
*/
func (api *API) WrapEnterWorldVerifier(verify auth.VerifyFunc) auth.VerifyFunc {
	return func(token, divisionID, characterName string) error {
		if api.TicketRevoked(token) {
			return auth.ErrExpired
		}
		return verify(token, divisionID, characterName)
	}
}

/*
================
handleAccountRevoke
================
*/
func (api *API) handleAccountRevoke(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.TrimSpace(r.Header.Get("Origin")) != "" {
		http.Error(w, "browser origin forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, revokeBodyLimit))
	if err != nil {
		http.Error(w, "invalid revocation body", http.StatusBadRequest)
		return
	}
	now := api.now()
	claims, err := api.agentSessionVerifier.VerifyAccountRevocation(strings.TrimSpace(string(body)), now)
	if err != nil || claims.ShardID != api.workerShardID {
		http.Error(w, "invalid account revocation", http.StatusUnauthorized)
		return
	}
	if !api.deliverRevocation(claims.KeyID+":"+claims.Nonce, claims.AccountID, claims.Epoch, claims.Expires, now) {
		http.Error(w, "revocation replay capacity reached", http.StatusTooManyRequests)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/*
================
deliverRevocation
================
*/
func (api *API) deliverRevocation(
	noticeKey string,
	accountID string,
	epoch uint64,
	expires int64,
	now time.Time,
) bool {
	api.revocations.mu.Lock()
	for key, deadline := range api.revocations.seenNotices {
		if deadline <= now.Unix() {
			delete(api.revocations.seenNotices, key)
		}
	}
	if _, exists := api.revocations.seenNotices[noticeKey]; exists {
		api.revocations.mu.Unlock()
		return true
	}
	if len(api.revocations.seenNotices) >= revokeReplayLimit {
		api.revocations.mu.Unlock()
		return false
	}
	api.revocations.seenNotices[noticeKey] = expires
	api.revocations.mu.Unlock()

	api.RevokeAccount(accountID, epoch)
	return true
}
