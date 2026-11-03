/*
===========================================================================

server_test.go - the Agent's title, routing and session flows

Shared fixtures for the package's tests (a two-division catalog, a fixed
clock, test signing keys) and the end-to-end login and routing cases.

===========================================================================
*/
package agentserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"opensro.online/server/internal/releaseprotocol"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/security/workload"
)

var testSessionPrivateKey = ed25519.NewKeyFromSeed(bytes.Repeat(
	[]byte{0x4b},
	ed25519.SeedSize,
))

var testSessionPublicKey = testSessionPrivateKey.Public().(ed25519.PublicKey)

var testSessionKeyID = fmt.Sprintf(
	"%x",
	sha256.Sum256(testSessionPublicKey),
)[:32]

/*
================
testWorkloadVerifier
================
*/
type testWorkloadVerifier struct{}

/*
================
Verify
================
*/
func (testWorkloadVerifier) Verify(
	_ context.Context,
	token string,
) (workload.IdentityClaims, error) {
	shardID := strings.TrimPrefix(token, "control-")
	if shardID == token || shardID == "" {
		return workload.IdentityClaims{}, fmt.Errorf("invalid test identity")
	}
	return workload.IdentityClaims{
		Namespace:    "sro",
		JobID:        "sro-gameworld-" + shardID,
		AllocationID: "alloc-" + shardID,
		Task:         "gameworld",
	}, nil
}

/*
================
testSessionSigner
================
*/
func testSessionSigner(t *testing.T) *auth.AgentSessionSigner {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session-keys.json")
	payload := fmt.Sprintf(
		"{\"activeKeyId\":%q,\"keys\":[{\"id\":%q,\"privateKey\":%q,\"createdAt\":\"2026-07-30T00:00:00Z\"}]}\n",
		testSessionKeyID,
		testSessionKeyID,
		base64.RawURLEncoding.EncodeToString(testSessionPrivateKey),
	)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewAgentSessionSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

/*
================
agentFixture
================
*/
type agentFixture struct {
	handler   http.Handler
	directory *shard.Directory
	readiness *readiness.Gate
	now       *time.Time
}

/*
================
newAgentFixture
================
*/
func newAgentFixture(
	t *testing.T,
	firstWorker http.Handler,
	secondWorker http.Handler,
	configure ...func(*Config),
) agentFixture {
	t.Helper()
	first := httptest.NewServer(firstWorker)
	t.Cleanup(first.Close)
	second := httptest.NewServer(secondWorker)
	t.Cleanup(second.Close)

	catalog, err := shard.NewCatalog([]shard.Definition{
		{
			ID:             "alpha",
			Name:           "Alpha",
			NativeServerID: 1,
			Capacity:       2,
			Default:        true,
			Enabled:        true,
			ControlURL:     first.URL,
			TransportURL:   "https://127.0.0.1:9001",
		},
		{
			ID:                 "beta",
			Name:               "Beta",
			NativeServerID:     2,
			Capacity:           3,
			Enabled:            true,
			ControlURL:         second.URL,
			TransportURL:       "https://127.0.0.1:9002",
			PublicTransportURL: "/shards/beta",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := shard.NewDirectory(catalog, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	accounts := loadTestAccounts(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	ready := readiness.NewGate()
	config := Config{
		Accounts:                accounts,
		Catalog:                 catalog,
		Directory:               directory,
		SessionSigner:           testSessionSigner(t),
		ControlIdentityVerifier: testWorkloadVerifier{},
		ControlNamespace:        "sro",
		Now:                     func() time.Time { return now },
		Readiness:               ready,
		// Logins prove routing, not bcrypt's speed; TestUnknownAccountRefusal
		// CostsWhatALoginDoes keeps one server at the production cost.
		BcryptCost: bcrypt.MinCost,
	}
	for _, apply := range configure {
		apply(&config)
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ready.Open()
	return agentFixture{
		handler:   server.Handler(),
		directory: directory,
		readiness: ready,
		now:       &now,
	}
}

/*
================
loadTestAccounts

The seed catalog the Agent loads in production, holding "tester". The
catalog refuses seed hashes below bcrypt.DefaultCost, so its answers carry
a bcrypt.MinCost hash of the same password instead: logins still resolve
through the real catalog, without paying a production-strength comparison
under the race detector.
================
*/
func loadTestAccounts(t *testing.T) AccountAuthority {
	t.Helper()
	hash, err := testSeedHash()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "accounts.json")
	payload := fmt.Sprintf(
		"[{\"id\":\"tester\",\"passwordHash\":%q}]",
		string(hash),
	)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cheap, err := bcrypt.GenerateFromPassword([]byte("123123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return cheapHashAccounts{Catalog: catalog, hash: cheap}
}

// testSeedHash is the catalog's "tester" hash, made once per test binary:
// it only has to pass the catalog's cost check, logins never compare it.
var testSeedHash = sync.OnceValues(func() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("123123"), bcrypt.DefaultCost)
})

/*
================
cheapHashAccounts

A catalog whose found accounts answer with hash.
================
*/
type cheapHashAccounts struct {
	*auth.Catalog
	hash []byte
}

/*
================
cheapHashAccounts.Credential
================
*/
func (accounts cheapHashAccounts) Credential(typedID string) (string, []byte, bool) {
	id, _, found := accounts.Catalog.Credential(typedID)
	if !found {
		return "", nil, false
	}
	return id, accounts.hash, true
}

/*
================
cheapHashAccounts.PasswordHash
================
*/
func (accounts cheapHashAccounts) PasswordHash(accountID string) ([]byte, bool) {
	if _, found := accounts.Catalog.PasswordHash(accountID); !found {
		return nil, false
	}
	return accounts.hash, true
}

/*
================
publishFixtureLease
================
*/
func publishFixtureLease(
	t *testing.T,
	fixture agentFixture,
	shardID string,
	instanceID string,
	sequence uint64,
	population int,
) {
	t.Helper()
	if err := fixture.directory.Publish(shard.Heartbeat{
		ShardID:       shardID,
		InstanceID:    instanceID,
		Sequence:      sequence,
		OnlinePlayers: population,
	}, *fixture.now); err != nil {
		t.Fatal(err)
	}
}

/*
================
performJSON
================
*/
func performJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	declareBrowser(request)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

/*
================
performControlJSON
================
*/
func performControlJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	shardID string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer control-"+shardID)
	request.Header.Set(shard.ControlShardHeader, shardID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

/*
================
decodeObject
================
*/
func decodeObject(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	return body
}

/*
================
TestReadinessStopsNewAgentRequests
================
*/
func TestReadinessStopsNewAgentRequests(t *testing.T) {
	fixture := newAgentFixture(
		t,
		http.NotFoundHandler(),
		http.NotFoundHandler(),
	)

	health := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		readiness.PathHealth,
		"",
		"",
	)
	if health.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", health.Code, health.Body)
	}
	ready := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		readiness.PathReady,
		"",
		"",
	)
	if ready.Code != http.StatusOK {
		t.Fatalf("ready = %d: %s", ready.Code, ready.Body)
	}

	fixture.readiness.Close()

	servers := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		"/title/servers",
		"",
		"",
	)
	if servers.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"title admission while draining = %d",
			servers.Code,
		)
	}
	if got := decodeObject(t, servers)["code"]; got != "PROCESS_DRAINING" {
		t.Fatalf("draining refusal code = %v", got)
	}
	notReady := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		readiness.PathReady,
		"",
		"",
	)
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness while draining = %d", notReady.Code)
	}
	stillLive := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		readiness.PathHealth,
		"",
		"",
	)
	if stillLive.Code != http.StatusOK {
		t.Fatalf("liveness while draining = %d", stillLive.Code)
	}
}

/*
================
TestServerListUsesFreshWorkerLease
================
*/
func TestServerListUsesFreshWorkerLease(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())

	response := performJSON(t, fixture.handler, http.MethodGet, "/title/servers", "", "")
	var servers []serverInfo
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	if servers[0].Operating || servers[0].OnlinePlayers != 0 {
		t.Fatalf("unleased shard advertised as live: %+v", servers[0])
	}

	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 2)
	response = performJSON(t, fixture.handler, http.MethodGet, "/title/servers", "", "")
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	if !servers[0].Operating || servers[0].OnlinePlayers != 2 {
		t.Fatalf("fresh lease not reflected: %+v", servers[0])
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	*fixture.now = fixture.now.Add(11 * time.Second)
	response = performJSON(t, fixture.handler, http.MethodGet, "/title/servers", "", "")
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	if servers[0].Operating || servers[0].OnlinePlayers != 0 {
		t.Fatalf("expired lease still advertised: %+v", servers[0])
	}
}

/*
================
TestClientsReceivePublicTransportRoute
================
*/
func TestClientsReceivePublicTransportRoute(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	publishFixtureLease(t, fixture, "beta", "worker-b", 1, 0)
	want := map[string]string{"alpha": "https://127.0.0.1:9001", "beta": "/shards/beta"}

	var servers []serverInfo
	response := performJSON(t, fixture.handler, http.MethodGet, "/title/servers", "", "")
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.TransportURL != want[server.ID] {
			t.Fatalf("directory advertises %q for %q, want %q", server.TransportURL, server.ID, want[server.ID])
		}
	}
	for id, transport := range want {
		login := performJSON(t, fixture.handler, http.MethodPost, "/title/login",
			`{"id":"tester","password":"123123","serverId":"`+id+`"}`, "")
		if body := decodeObject(t, login); body["ok"] != true || body["transportUrl"] != transport {
			t.Fatalf("login to %q = %s, want transportUrl %q", id, login.Body.String(), transport)
		}
	}
}

/*
================
TestLoginRefusesDisabledShardExplicitly
================
*/
func TestLoginRefusesDisabledShardExplicitly(t *testing.T) {
	catalog, err := shard.NewCatalog([]shard.Definition{{
		ID:             "disabled",
		Name:           "Disabled",
		NativeServerID: 1,
		Capacity:       10,
		Default:        true,
		Enabled:        false,
		ControlURL:     "http://127.0.0.1:9001",
		TransportURL:   "https://127.0.0.1:9002",
	}})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := shard.NewDirectory(catalog, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	accounts := loadTestAccounts(t)
	ready := readiness.NewGate()
	ready.Open()
	server, err := New(Config{
		Accounts:                accounts,
		Catalog:                 catalog,
		Directory:               directory,
		SessionSigner:           testSessionSigner(t),
		ControlIdentityVerifier: testWorkloadVerifier{},
		ControlNamespace:        "sro",
		Now: func() time.Time {
			return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
		},
		Readiness:  ready,
		BcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/title/login",
		`{"id":"tester","password":"123123","serverId":"disabled"}`,
		"",
	)
	body := decodeObject(t, response)
	if body["ok"] != false || body["code"] != "SHARD_OFFLINE" {
		t.Fatalf("disabled shard login = %#v, want SHARD_OFFLINE", body)
	}
	servers := performJSON(t, server.Handler(), http.MethodGet, "/title/servers", "", "")
	var rows []serverInfo
	if err := json.Unmarshal(servers.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("disabled shard listed: %+v", rows)
	}
}

/*
================
TestLoginBindsProxyRoutingToSelectedShard
================
*/
func TestLoginBindsProxyRoutingToSelectedShard(t *testing.T) {
	var alphaCalls, betaCalls int
	worker := func(name string, calls *int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			(*calls)++
			claims, err := auth.VerifyAgentSession(
				map[string]ed25519.PublicKey{
					testSessionKeyID: testSessionPublicKey,
				},
				strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
				time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
			)
			if err != nil {
				t.Errorf("%s worker token: %v", name, err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"worker": name,
				"shard":  claims.ShardID,
			})
		})
	}
	fixture := newAgentFixture(t, worker("alpha", &alphaCalls), worker("beta", &betaCalls))
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	publishFixtureLease(t, fixture, "beta", "worker-b", 1, 0)

	login := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/title/login",
		`{"id":"tester","password":"123123","serverId":"alpha",`+
			`"divisionId":"beta","channelId":"normal"}`,
		"",
	)
	body := decodeObject(t, login)
	if body["ok"] != true || body["divisionId"] != "alpha" || body["nativeServerName"] != "Alpha" {
		t.Fatalf("login = %s", login.Body.String())
	}
	token, _ := body["sessionToken"].(string)
	if token == "" {
		t.Fatal("login omitted sessionToken")
	}

	proxy := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/character/list?divisionId=beta",
		`{"divisionId":"beta"}`,
		token,
	)
	proxyBody := decodeObject(t, proxy)
	if proxyBody["worker"] != "alpha" || proxyBody["shard"] != "alpha" {
		t.Fatalf("proxy escaped token shard: %s", proxy.Body.String())
	}
	if alphaCalls != 1 || betaCalls != 0 {
		t.Fatalf("worker calls alpha=%d beta=%d", alphaCalls, betaCalls)
	}
	areaEntry := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/character/enter-area",
		`{"characterName":"GMHero","areaSlug":"manyang-lab"}`,
		token,
	)
	areaEntryBody := decodeObject(t, areaEntry)
	if areaEntryBody["worker"] != "alpha" || areaEntryBody["shard"] != "alpha" {
		t.Fatalf("area-entry proxy escaped token shard: %s", areaEntry.Body.String())
	}
	if alphaCalls != 2 || betaCalls != 0 {
		t.Fatalf("worker calls after area entry alpha=%d beta=%d", alphaCalls, betaCalls)
	}
	areaExit := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/character/leave-area",
		`{"characterName":"GMHero"}`,
		token,
	)
	areaExitBody := decodeObject(t, areaExit)
	if areaExitBody["worker"] != "alpha" || areaExitBody["shard"] != "alpha" {
		t.Fatalf("area-exit proxy escaped token shard: %s", areaExit.Body.String())
	}
	if alphaCalls != 3 || betaCalls != 0 {
		t.Fatalf("worker calls after area exit alpha=%d beta=%d", alphaCalls, betaCalls)
	}
	fixtureReset := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		agentapi.BenchmarkFixtureResetPath,
		`{"characterName":"Test2","fixtureId":"fixture","movementMode":3,"spawn":{"regionId":28234,"x":1756,"y":7,"z":525,"angle":16384}}`,
		token,
	)
	fixtureResetBody := decodeObject(t, fixtureReset)
	if fixtureResetBody["worker"] != "alpha" || fixtureResetBody["shard"] != "alpha" {
		t.Fatalf("fixture-reset proxy escaped token shard: %s", fixtureReset.Body.String())
	}
	if alphaCalls != 4 || betaCalls != 0 {
		t.Fatalf("worker calls after fixture reset alpha=%d beta=%d", alphaCalls, betaCalls)
	}
	transportToken := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/auth/transport-token",
		`{}`,
		token,
	)
	transportTokenBody := decodeObject(t, transportToken)
	if transportTokenBody["worker"] != "alpha" || transportTokenBody["shard"] != "alpha" {
		t.Fatalf("transport-token proxy escaped token shard: %s", transportToken.Body.String())
	}
	if alphaCalls != 5 || betaCalls != 0 {
		t.Fatalf("worker calls after transport mint alpha=%d beta=%d", alphaCalls, betaCalls)
	}

	tokenParts := strings.Split(token, ".")
	macBytes := []byte(tokenParts[5])
	if macBytes[0] == 'A' {
		macBytes[0] = 'B'
	} else {
		macBytes[0] = 'A'
	}
	tokenParts[5] = string(macBytes)
	tampered := strings.Join(tokenParts, ".")
	refused := performJSON(
		t,
		fixture.handler,
		http.MethodGet,
		"/character/list",
		"",
		tampered,
	)
	if refused.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token status = %d", refused.Code)
	}
}

/*
================
TestLoginRefusesOfflineAndFullShard
================
*/
func TestLoginRefusesOfflineAndFullShard(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	loginBody := `{"id":"tester","password":"123123","serverId":"alpha"}`

	response := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/title/login",
		loginBody,
		"",
	)
	if got := decodeObject(t, response)["code"]; got != "SHARD_OFFLINE" {
		t.Fatalf("offline login code = %v", got)
	}

	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 2)
	response = performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/title/login",
		loginBody,
		"",
	)
	if got := decodeObject(t, response)["code"]; got != "SHARD_FULL" {
		t.Fatalf("full login code = %v", got)
	}
}

/*
================
TestAdmissionRefusalsNameMaintenance

With the port-only maintenance message switch on, this Agent's refusals
for a shard that is not operating name the maintenance: the title login
code and message, and the shard-request refusal as a JSON code the
client's admission mint can read. Off, both keep today's answers:
SHARD_OFFLINE at login and the plain "shard unavailable" 503.
================
*/
func TestAdmissionRefusalsNameMaintenance(t *testing.T) {
	maintenance := "The server is under maintenance. Please try again later."
	loginBody := `{"id":"tester","password":"123123","serverId":"alpha"}`

	// A token from a leased, operating shard (the fixtures share the test
	// signer and clock, so it verifies on both unleased fixtures below).
	leased := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	publishFixtureLease(t, leased, "alpha", "worker-a", 1, 0)
	login := performJSON(t, leased.handler, http.MethodPost, "/title/login", loginBody, "")
	token, _ := decodeObject(t, login)["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %s", login.Body.String())
	}

	on := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler(), func(config *Config) {
		config.MaintenanceMessage = true
	})
	body := decodeObject(t, performJSON(t, on.handler, http.MethodPost, "/title/login", loginBody, ""))
	if body["code"] != "MAINTENANCE" || body["message"] != maintenance {
		t.Fatalf("maintenance login refusal = %#v", body)
	}
	refused := performJSON(t, on.handler, http.MethodPost, "/character/list", `{}`, token)
	if refused.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance shard refusal status = %d", refused.Code)
	}
	body = decodeObject(t, refused)
	if body["ok"] != false || body["code"] != "MAINTENANCE" || body["message"] != maintenance {
		t.Fatalf("maintenance shard refusal = %#v", body)
	}

	off := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	body = decodeObject(t, performJSON(t, off.handler, http.MethodPost, "/title/login", loginBody, ""))
	if body["code"] != "SHARD_OFFLINE" || body["message"] != "The selected server is not operating." {
		t.Fatalf("default login refusal = %#v", body)
	}
	refused = performJSON(t, off.handler, http.MethodPost, "/character/list", `{}`, token)
	if refused.Code != http.StatusServiceUnavailable || refused.Body.String() != "shard unavailable\n" {
		t.Fatalf("default shard refusal = %d %q", refused.Code, refused.Body.String())
	}
}

/*
================
TestHeartbeatRequiresControlSecret
================
*/
func TestHeartbeatRequiresControlSecret(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	payload := `{"ShardID":"alpha","InstanceID":"worker-a",` +
		`"Sequence":1,"OnlinePlayers":0}`

	response := performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/internal/cluster/shards/heartbeat",
		payload,
		"wrong-control-secret-that-is-long",
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized heartbeat status = %d", response.Code)
	}
	response = performJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/internal/cluster/shards/heartbeat",
		payload,
		"control-alpha",
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"unrelated workload identity authorized a shard directly: %d",
			response.Code,
		)
	}
	response = performControlJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/internal/cluster/shards/heartbeat",
		payload,
		"alpha",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized heartbeat = %d %s", response.Code, response.Body.String())
	}
	response = performControlJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/internal/cluster/shards/heartbeat",
		`{"ShardID":"beta","InstanceID":"worker-b",`+
			`"Sequence":1,"OnlinePlayers":0}`,
		"alpha",
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"alpha credential wrote beta heartbeat: %d %s",
			response.Code,
			response.Body.String(),
		)
	}
	response = performControlJSON(
		t,
		fixture.handler,
		http.MethodPost,
		"/internal/cluster/shards/release",
		`{"shardId":"alpha","instanceId":"worker-a"}`,
		"alpha",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized release = %d %s", response.Code, response.Body.String())
	}
	if status := fixture.directory.Snapshot(*fixture.now)[0]; status.Operating {
		t.Fatalf("released shard still operating: %+v", status)
	}
}

/*
================
TestAccountDirectoryExposesIDsOnlyToGameWorldControl
================
*/
func TestAccountDirectoryExposesIDsOnlyToGameWorldControl(
	t *testing.T,
) {
	fixture := newAgentFixture(
		t,
		http.NotFoundHandler(),
		http.NotFoundHandler(),
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/internal/accounts",
		nil,
	)
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated directory = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/internal/accounts",
		nil,
	)
	request.Header.Set("Authorization", "Bearer control-alpha")
	request.Header.Set(shard.ControlShardHeader, "alpha")
	recorder = httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("account directory = %d: %s", recorder.Code, recorder.Body)
	}
	var response struct {
		AccountIDs []string `json:"accountIds"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.AccountIDs) != 1 || response.AccountIDs[0] != "tester" {
		t.Fatalf("account ids = %v, want tester", response.AccountIDs)
	}
	if strings.Contains(recorder.Body.String(), "password") ||
		strings.Contains(recorder.Body.String(), "$2") {
		t.Fatalf("account directory leaked credentials: %s", recorder.Body)
	}
}

/*
================
TestDecodeJSONRejectsOversizedRequest
================
*/
func TestDecodeJSONRejectsOversizedRequest(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	oversized := bytes.Repeat([]byte("x"), int(maxRequestBytes)+1)
	request := httptest.NewRequest(
		http.MethodPost,
		"/title/login",
		bytes.NewReader(oversized),
	)
	declareBrowser(request)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if got := decodeObject(t, response)["code"]; got != "BAD_REQUEST" {
		t.Fatalf("oversized login code = %v", got)
	}
}

/*
================
declareBrowser

Declares the release protocol, as every browser request does.
================
*/
func declareBrowser(r *http.Request) {
	r.Header.Set(releaseprotocol.Header, strconv.Itoa(releaseprotocol.Current))
}
