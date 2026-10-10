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

// loginStartingRetrySeconds paces a login retried while its shard starts at
// the login budget's own refill (login_limiter.go), so waiting never spends
// the burst and turns into RATE_LIMITED.
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

The public server list rows of a directory snapshot.
================
*/
func serverRows(statuses []shard.Status) []serverInfo {
	rows := make([]serverInfo, 0, len(statuses))
	for _, status := range statuses {
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
a lease: every shard listed, none operating, so the title shows each one as
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
	refuse := func(nativeStatus int, code, message string) {
		server.history.Record(history.Event{Kind: "login_refused", Code: code, Category: "expected", Message: message, Fields: map[string]string{"claimedAccount": boundedLoginName(request.ID)}})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "nativeTitleStatus": nativeStatus,
			"code": code, "message": message,
		})
	}
	passwordRefusal := func(failed bool) {
		server.history.Record(history.Event{Kind: "login_refused", Code: "invalid_credentials", Category: "expected", Fields: map[string]string{"claimedAccount": boundedLoginName(request.ID)}})
		argument, admitted := server.passwordFailures.update(clientIP(r), request.ID, server.now(), failed, false)
		if !admitted {
			refuse(5, "RATE_LIMITED", "Credential failure tracking is at capacity.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "nativeTitleStatus": 2, "nativeTitleArgument": argument, "code": "INVALID_CREDENTIALS", "message": "The id or password is incorrect."})
	}
	if err := decodeJSON(r, &request); err != nil {
		refuse(5, "BAD_REQUEST", "Malformed login request.")
		return
	}
	if !server.loginAttempts.Allow(clientIP(r)) {
		server.history.Record(history.Event{Kind: "login_refused", Category: "expected", Code: "rate_limited", Fields: map[string]string{"claimedAccount": boundedLoginName(request.ID)}})
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
	if !status.Operating && status.Starting {
		// A leased GameWorld still loading its worlds: the client waits and
		// logs in again (server-starting.ts) instead of failing. Retry-After
		// matches the login budget's refill, so the wait never spends it.
		server.history.Record(history.Event{Kind: "login_refused", Code: readiness.CodeStarting, Category: "expected", Fields: map[string]string{"claimedAccount": boundedLoginName(request.ID)}})
		w.Header().Set("Retry-After", strconv.Itoa(loginStartingRetrySeconds))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "nativeTitleStatus": 5,
			"code": readiness.CodeStarting, "message": "The server is starting.",
			"retryAfter": loginStartingRetrySeconds,
		})
		return
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
		server.history.Record(history.Event{Kind: "login_refused", Category: "expected", Code: "authentication_busy", Fields: map[string]string{"claimedAccount": boundedLoginName(request.ID)}})
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

	token, err := server.sessionSigner.Mint(
		accountID,
		definition.ID,
		server.now().Add(auth.AgentSessionLifetime),
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
	claims, ok := server.bearerClaims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	definition, ok := server.catalog.Resolve(claims.ShardID)
	if !ok || !definition.Enabled {
		http.Error(w, "shard unavailable", http.StatusServiceUnavailable)
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
		http.Error(w, "shard unavailable", http.StatusServiceUnavailable)
		return
	}

	target, err := shardRequestURL(definition.ControlURL, r)
	if err != nil {
		http.Error(w, "shard route invalid", http.StatusBadGateway)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || int64(len(body)) > maxRequestBytes {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	request, err := http.NewRequestWithContext(
		r.Context(),
		r.Method,
		target,
		bytes.NewReader(body),
	)
	if err != nil {
		http.Error(w, "shard request failed", http.StatusBadGateway)
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
		http.Error(w, "shard unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || int64(len(responseBody)) > maxResponseBytes {
		http.Error(w, "invalid shard response", http.StatusBadGateway)
		return
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
