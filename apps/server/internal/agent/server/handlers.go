/*
===========================================================================

handlers.go - the Agent's shard listing, login and shard control routes

===========================================================================
*/
package agentserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"opensro.online/server/internal/releaseprotocol"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
)

// loginStartingRetrySeconds paces a login retried while its shard starts.
// The starting answer is not charged to the login budget; the pace still
// matches the budget's refill (login_limiter.go) so a retry that lands just
// as the shard opens is never refused as RATE_LIMITED.
const loginStartingRetrySeconds = int(loginAttemptRefill / time.Second)

/*
================
serverInfo
================
*/
type serverInfo struct {
	ID             string `json:"id"`
	NativeServerID uint16 `json:"nativeServerId"`
	NativeFarmID   uint16 `json:"nativeFarmId"`
	Name           string `json:"name"`
	IsTest         bool   `json:"isTest"`
	OnlinePlayers  int    `json:"onlinePlayers"`
	Capacity       int    `json:"capacity"`
	Operating      bool   `json:"operating"`
	TransportURL   string `json:"transportUrl"`
}

/*
================
handleServers
================
*/
func (server *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, serverRows(server.directory.Snapshot(server.now())))
}

/*
================
serverRows

The public server list rows of a directory snapshot: the enabled shards.
A disabled shard can never hold a lease or admit a login, so it is not a
row. An enabled shard with no live GameWorld stays listed, and the title
shows it as Check.
================
*/
func serverRows(statuses []shard.Status) []serverInfo {
	rows := make([]serverInfo, 0, len(statuses))
	for _, status := range statuses {
		if !status.Enabled {
			continue
		}
		rows = append(rows, serverInfo{
			ID:             status.ID,
			NativeServerID: status.NativeServerID,
			NativeFarmID:   status.NativeFarmID,
			Name:           status.Name,
			IsTest:         status.Test,
			OnlinePlayers:  status.OnlinePlayers,
			Capacity:       status.Capacity,
			Operating:      status.Operating,
			TransportURL:   status.AdvertisedTransportURL(),
		})
	}
	return rows
}

/*
================
OfflineServerList

The /title/servers body this catalog's Agent serves while no GameWorld has
a lease: every enabled shard listed, none operating, so the title shows each as
native "Check" (CPSTitle 0x747E1F). The release edge serves it in place of
the Agent while the Agent itself is down for maintenance.
================
*/
func OfflineServerList(catalog *shard.Catalog, now time.Time) ([]byte, error) {
	directory, err := shard.NewDirectory(catalog, time.Minute)
	if err != nil {
		return nil, err
	}
	return json.Marshal(serverRows(directory.Snapshot(now)))
}

/*
================
shardStarting

Whether serverID names an enabled shard whose live owner holds its lease
but has not opened admission yet.
================
*/
func (server *Server) shardStarting(serverID string) bool {
	definition, ok := server.catalog.Resolve(serverID)
	if !ok || !definition.Enabled {
		return false
	}
	for _, status := range server.directory.Snapshot(server.now()) {
		if status.ID == definition.ID {
			return status.Starting
		}
	}
	return false
}

/*
================
handleLogin
================
*/
func (server *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		ID         string `json:"id"`
		Password   string `json:"password"`
		ServerID   string `json:"serverId"`
		DivisionID string `json:"divisionId"`
		ChannelID  string `json:"channelId"`
	}
	// Every refusal below writes one paced log line (refusal_log.go) beside
	// the history event it already recorded.
	logLogin := func(code string, status int) {
		server.logRefusal(r, refusal{Route: loginPath, Code: code, Status: status,
			Account: server.loggableAccount(request.ID), Shard: server.loggableShard(request.ServerID)})
	}
	refuse := func(nativeStatus int, code, message string) {
		server.history.Record(history.Event{Kind: "login_refused", Code: code, Category: "expected", Message: message, Fields: map[string]string{"claimedAccount": server.loggableAccount(request.ID)}})
		logLogin(code, http.StatusOK)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "nativeTitleStatus": nativeStatus,
			"code": code, "message": message,
		})
	}
	passwordRefusal := func(failed bool) {
		server.history.Record(history.Event{Kind: "login_refused", Code: "invalid_credentials", Category: "expected", Fields: map[string]string{"claimedAccount": server.loggableAccount(request.ID)}})
		argument, admitted := server.passwordFailures.update(clientIP(r), request.ID, server.now(), failed, false)
		if !admitted {
			refuse(5, "RATE_LIMITED", "Credential failure tracking is at capacity.")
			return
		}
		logLogin("INVALID_CREDENTIALS", http.StatusOK)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "nativeTitleStatus": 2, "nativeTitleArgument": argument, "code": "INVALID_CREDENTIALS", "message": "The id or password is incorrect."})
	}
	if err := decodeJSON(r, &request); err != nil {
		refuse(5, "BAD_REQUEST", "Malformed login request.")
		return
	}
	// A leased GameWorld still loading its worlds: the client waits and logs
	// in again (server-starting.ts) instead of failing. Answered before the
	// per-address login budget: it tests no credential, so players sharing an
	// address cannot drain the budget while the shard starts.
	// It writes no history: it is not a credential refusal, and an
	// unthrottled answer must not become an unbounded history writer. Its
	// log line is paced like every refusal line.
	if server.shardStarting(request.ServerID) {
		logLogin(readiness.CodeStarting, http.StatusServiceUnavailable)
		w.Header().Set("Retry-After", strconv.Itoa(loginStartingRetrySeconds))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "nativeTitleStatus": 5,
			"code": readiness.CodeStarting, "message": "The server is starting.",
			"retryAfter": loginStartingRetrySeconds,
		})
		return
	}
	if !server.loginAttempts.Allow(clientIP(r)) {
		server.history.Record(history.Event{Kind: "login_refused", Category: "expected", Code: "rate_limited", Fields: map[string]string{"claimedAccount": server.loggableAccount(request.ID)}})
		logLogin("RATE_LIMITED", http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "6")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok": false, "nativeTitleStatus": 5,
			"code": "RATE_LIMITED", "message": "Too many login attempts.",
		})
		return
	}
	definition, ok := server.catalog.Resolve(request.ServerID)
	if !ok {
		refuse(5, "UNKNOWN_SHARD", "Unknown server.")
		return
	}
	if !definition.Enabled {
		refuse(5, "SHARD_OFFLINE", "The selected server is not operating.")
		return
	}
	var status shard.Status
	for _, candidate := range server.directory.Snapshot(server.now()) {
		if candidate.ID == definition.ID {
			status = candidate
			break
		}
	}
	if !status.Operating {
		refuse(5, "SHARD_OFFLINE", "The selected server is not operating.")
		return
	}
	if status.OnlinePlayers >= status.Capacity {
		refuse(5, "SHARD_FULL", "The selected server is full.")
		return
	}
	if request.ID == "" || request.Password == "" ||
		len(request.Password) > maxPasswordBytes {
		passwordRefusal(true)
		return
	}
	if argument, _ := server.passwordFailures.update(clientIP(r), request.ID, server.now(), false, false); argument&0xffff >= passwordFailureLimit {
		w.Header().Set("Retry-After", "60")
		passwordRefusal(false)
		return
	}

	select {
	case server.passwordSlots <- struct{}{}:
		defer func() { <-server.passwordSlots }()
	default:
		server.history.Record(history.Event{Kind: "login_refused", Category: "expected", Code: "authentication_busy", Fields: map[string]string{"claimedAccount": server.loggableAccount(request.ID)}})
		logLogin("AUTHENTICATION_BUSY", http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "6")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok": false, "nativeTitleStatus": 5,
			"code": "RATE_LIMITED", "message": "Too many login attempts.",
		})
		return
	}
	// The session carries the stored id, which owns the characters, whatever
	// case the player typed.
	accountID, hash, found := server.accounts.Credential(request.ID)
	if !found {
		hash = server.dummyHash
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(request.Password)); !found || err != nil {
		passwordRefusal(true)
		return
	}
	server.passwordFailures.update(clientIP(r), request.ID, server.now(), false, true)
	server.upgradePasswordCost(r.Context(), accountID, hash, request.Password)

	epoch := uint64(1)
	if epochAuth, ok := server.accounts.(sessionEpochAuthority); ok {
		if current, active := epochAuth.SessionEpoch(accountID); active {
			epoch = current
		}
	}
	server.revokedMu.Lock()
	if minEpoch := server.accountMinEpoch[accountID]; minEpoch > epoch {
		epoch = minEpoch
	}
	server.revokedMu.Unlock()
	token, err := server.sessionSigner.MintWithEpoch(
		accountID,
		definition.ID,
		server.now().Add(auth.AgentSessionLifetime),
		epoch,
	)
	if err != nil {
		refuse(5, "INTERNAL", "Token generation failed.")
		return
	}
	server.setBrowserSession(w, r, token, false)
	server.history.Record(history.Event{Kind: "login_succeeded", Code: "authenticated", Category: "expected", Account: accountID, Shard: definition.ID})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"nativeTitleStatus": 1,
		"nativeServerId":    definition.NativeServerID,
		"nativeServerName":  definition.Name,
		"sessionToken":      token,
		"divisionId":        definition.ID,
		"transportUrl":      definition.AdvertisedTransportURL(),
		"nextScene":         "character-select",
	})
}

/*
================
handleHeartbeat
================
*/
func (server *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controlShardID, authorized := server.authorizedControlShard(r)
	if !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var heartbeat shard.Heartbeat
	if err := decodeJSON(r, &heartbeat); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "BAD_REQUEST"})
		return
	}
	if heartbeat.ShardID != controlShardID {
		http.Error(w, "control shard mismatch", http.StatusForbidden)
		return
	}
	if err := server.directory.Publish(heartbeat, server.now()); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "LEASE_REFUSED", "message": err.Error(),
		})
		return
	}
	server.syncShardRevocations(heartbeat)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/*
================
handleAccountDirectory
================
*/
func (server *Server) handleAccountDirectory(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, authorized := server.authorizedControlShard(r); !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accountIds": server.accounts.IDs(),
	})
}

/*
================
handleLeaseRelease
================
*/
func (server *Server) handleLeaseRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controlShardID, authorized := server.authorizedControlShard(r)
	if !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var release struct {
		ShardID    string `json:"shardId"`
		InstanceID string `json:"instanceId"`
	}
	if err := decodeJSON(r, &release); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "code": "BAD_REQUEST",
		})
		return
	}
	if release.ShardID != controlShardID {
		http.Error(w, "control shard mismatch", http.StatusForbidden)
		return
	}
	if err := server.directory.Release(release.ShardID, release.InstanceID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "LEASE_RELEASE_REFUSED", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/*
================
handleShardRequest
================
*/
func (server *Server) handleShardRequest(w http.ResponseWriter, r *http.Request) {
	// Each local refusal and each refusal the GameWorld answers writes one
	// line (refusal_log.go); the account is the verified session's.
	account, shardID := "", ""
	refuse := func(status int, code, message string) {
		server.logRefusal(r, refusal{Route: r.URL.Path, Code: code, Status: status, Account: account,
			Shard: shardID, Kind: requestRefusedKind})
		http.Error(w, message, status)
	}
	claims, ok := server.bearerClaims(r)
	if !ok {
		refuse(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}
	// The line names the stored account the verified session resolves to
	// (loggableAccount), never anything read from the header itself.
	account, shardID = server.loggableAccount(claims.AccountID), server.loggableShard(claims.ShardID)
	definition, ok := server.catalog.Resolve(claims.ShardID)
	if !ok || !definition.Enabled {
		refuse(http.StatusServiceUnavailable, "SHARD_UNAVAILABLE", "shard unavailable")
		return
	}
	operating := false
	for _, status := range server.directory.Snapshot(server.now()) {
		if status.ID == claims.ShardID {
			operating = status.Operating
			break
		}
	}
	if !operating {
		refuse(http.StatusServiceUnavailable, "SHARD_OFFLINE", "shard unavailable")
		return
	}

	target, err := shardRequestURL(definition.ControlURL, r)
	if err != nil {
		refuse(http.StatusBadGateway, "SHARD_ROUTE_INVALID", "shard route invalid")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || int64(len(body)) > maxRequestBytes {
		refuse(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request too large")
		return
	}
	request, err := http.NewRequestWithContext(
		r.Context(),
		r.Method,
		target,
		bytes.NewReader(body),
	)
	if err != nil {
		refuse(http.StatusBadGateway, "SHARD_FAILED", "shard request failed")
		return
	}
	request.Header.Set("Authorization", r.Header.Get("Authorization"))
	// The shard enforces the release protocol too; the agent already did.
	request.Header.Set(releaseprotocol.Header, r.Header.Get(releaseprotocol.Header))
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := server.client.Do(request)
	if err != nil {
		refuse(http.StatusBadGateway, "SHARD_UNREACHABLE", "shard unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || int64(len(responseBody)) > maxResponseBytes {
		refuse(http.StatusBadGateway, "SHARD_RESPONSE_INVALID", "invalid shard response")
		return
	}
	if code, refused := relayedRefusalCode(response.StatusCode, responseBody); refused {
		server.logRefusal(r, refusal{Route: r.URL.Path, Code: code, Status: response.StatusCode, Account: account,
			Shard: shardID, Kind: requestRefusedKind, Source: "gameworld"})
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	server.rememberBrowserCharacter(w, r, body, responseBody, response.StatusCode)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

/*
================
shardRequestURL
================
*/
func shardRequestURL(controlURL string, request *http.Request) (string, error) {
	if strings.Contains(request.URL.Path, "..") {
		return "", fmt.Errorf("unsafe route path")
	}
	base := strings.TrimSuffix(controlURL, "/")
	target := base + request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		target += "?" + request.URL.RawQuery
	}
	return target, nil
}
