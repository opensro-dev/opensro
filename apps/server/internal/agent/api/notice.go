/*
===========================================================================

notice.go - the authenticated local operator announcement endpoint

Only a short-lived request signed by the operator's private ring may publish.
Retransmission is idempotent until expiry; browser and proxy requests are refused.

===========================================================================
*/
package agentapi

import (
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	NoticePath        = "/internal/operations/notice"
	noticeBodyLimit   = 2048
	noticeReplayLimit = 256
)

/*
================
noticePublisher
================
*/
type noticePublisher struct {
	mu      sync.Mutex
	seen    map[string]int64
	publish func(string)
}

/*
================
InstallNoticePublisher

Called by the composition root before the control listener starts.
================
*/
func (api *API) InstallNoticePublisher(publish func(string)) {
	api.notices = &noticePublisher{publish: publish, seen: make(map[string]int64)}
}

/*
================
handleNotice
================
*/
func (api *API) handleNotice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !localDiagnosticsRequest(r) {
		http.Error(w, "local operator request required", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, noticeBodyLimit))
	if err != nil {
		http.Error(w, "invalid notice body", http.StatusBadRequest)
		return
	}
	now := api.now()
	claims, err := api.agentSessionVerifier.VerifyNotice(string(body), now)
	if err != nil || claims.ShardID != api.workerShardID {
		http.Error(w, "invalid operator notice", http.StatusUnauthorized)
		return
	}
	if !api.notices.deliver(string(body), claims.Message, claims.Expires, now) {
		http.Error(w, "notice capacity reached", http.StatusTooManyRequests)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/*
================
deliver

Serializes acceptance with delivery so concurrent HTTP retries publish once.
================
*/
func (publisher *noticePublisher) deliver(token, message string, expires int64, now time.Time) bool {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	for key, deadline := range publisher.seen {
		if deadline <= now.Unix() {
			delete(publisher.seen, key)
		}
	}
	if _, exists := publisher.seen[token]; exists {
		return true
	}
	if len(publisher.seen) >= noticeReplayLimit {
		return false
	}
	publisher.seen[token] = expires
	publisher.publish(message)
	return true
}
