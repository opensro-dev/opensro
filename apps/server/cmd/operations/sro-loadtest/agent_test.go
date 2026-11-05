/*
===========================================================================

agent_test.go - the bot's Agent contract against a strict fake

Slice 1's bot only ever faced the transport probe's handshake; the real
Agent refuses a request that does not declare the release protocol
(426) and reads shard requests by bearer claim, not cookie. This fake
enforces both, so the contract the live stack will demand is pinned
here instead of discovered during a staircase run.
===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/releaseprotocol"
)

// fakeAgent is the Agent's browser surface with its two gates on:
// the release-protocol declaration and the session bearer.
type fakeAgent struct {
	mu             sync.Mutex
	passwords      map[string]string
	rosters        map[string][]string
	tokens         map[string]string
	createRefusals map[string]int
	loginBearer    string
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{
		passwords:      map[string]string{},
		rosters:        map[string][]string{},
		tokens:         map[string]string{},
		createRefusals: map[string]int{},
	}
}

/*
================
handler
================
*/
func (f *fakeAgent) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/title/login", f.login)
	mux.HandleFunc("/character/list", f.list)
	mux.HandleFunc("/character/create", f.create)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(releaseprotocol.Header) != strconv.Itoa(releaseprotocol.Current) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUpgradeRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "client-outdated", "protocol": releaseprotocol.Current})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (f *fakeAgent) bearerAccount(r *http.Request) string {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens[token]
}

/*
================
login
================
*/
func (f *fakeAgent) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.loginBearer = r.Header.Get("Authorization")
	want, known := f.passwords[body.ID]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !known || want != body.Password {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": "INVALID_CREDENTIALS"})
		return
	}
	token := "tok-" + body.ID
	f.mu.Lock()
	f.tokens[token] = body.ID
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "sessionToken": token})
}

/*
================
list
================
*/
func (f *fakeAgent) list(w http.ResponseWriter, r *http.Request) {
	account := f.bearerAccount(r)
	if account == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	names := append([]string(nil), f.rosters[account]...)
	f.mu.Unlock()
	characters := make([]map[string]string, 0, len(names))
	for _, name := range names {
		characters = append(characters, map[string]string{"name": name})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"characters": characters})
}

/*
================
create
================
*/
func (f *fakeAgent) create(w http.ResponseWriter, r *http.Request) {
	account := f.bearerAccount(r)
	if account == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		CharacterName string `json:"characterName"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	code, refused := f.createRefusals[body.CharacterName]
	if !refused {
		f.rosters[account] = append(f.rosters[account], body.CharacterName)
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if refused {
		_ = json.NewEncoder(w).Encode(map[string]any{"action": 1, "nativeResult": 0, "nativeErrorCode": code})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"action": 1, "nativeResult": 1})
}

/*
================
TestAgentContractLoginAndRoster

Login declares the protocol and sends no bearer (there is none yet);
the roster is then read under the bearer the login minted.
================
*/
func TestAgentContractLoginAndRoster(t *testing.T) {
	fake := newFakeAgent()
	fake.passwords["load0001"] = "pw-123456"
	fake.rosters["load0001"] = []string{"Load0001"}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	client, err := newAgentClient(server.URL, "global-official", "global-official", 5*time.Second, botSourceIP(3))
	if err != nil {
		t.Fatalf("newAgentClient: %v", err)
	}
	if err := client.login("load0001", "pw-123456"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if fake.loginBearer != "" {
		t.Fatalf("login carried a bearer %q before one was minted", fake.loginBearer)
	}
	names, err := client.characterNames()
	if err != nil {
		t.Fatalf("characterNames: %v", err)
	}
	if len(names) != 1 || names[0] != "Load0001" {
		t.Fatalf("characterNames = %v, want [Load0001]", names)
	}
}

/*
================
TestAgentLoginRefusals

A wrong password names its code; a stack speaking another release
protocol is named as such, not as a credential failure.
================
*/
func TestAgentLoginRefusals(t *testing.T) {
	fake := newFakeAgent()
	fake.passwords["load0001"] = "pw-123456"
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	client, err := newAgentClient(server.URL, "global-official", "global-official", 5*time.Second, botSourceIP(1))
	if err != nil {
		t.Fatalf("newAgentClient: %v", err)
	}
	if err := client.login("load0001", "wrong-password"); err == nil || !strings.Contains(err.Error(), "INVALID_CREDENTIALS") {
		t.Fatalf("wrong password error = %v, want INVALID_CREDENTIALS named", err)
	}

	outdated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = fmt.Fprint(w, `{"error":"client-outdated","protocol":99}`)
	}))
	defer outdated.Close()
	stale, err := newAgentClient(outdated.URL, "global-official", "global-official", 5*time.Second, botSourceIP(1))
	if err != nil {
		t.Fatalf("newAgentClient: %v", err)
	}
	if err := stale.login("load0001", "pw-123456"); err == nil || !strings.Contains(err.Error(), "release protocol") {
		t.Fatalf("outdated stack error = %v, want the release protocol named", err)
	}
}

/*
================
TestAgentCreateCharacter

Create answers the native result pair: 1 creates, anything else is a
refusal carrying its native error code.
================
*/
func TestAgentCreateCharacter(t *testing.T) {
	fake := newFakeAgent()
	fake.passwords["load0001"] = "pw-123456"
	fake.createRefusals["Taken"] = 0x43
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	client, err := newAgentClient(server.URL, "global-official", "global-official", 5*time.Second, botSourceIP(2))
	if err != nil {
		t.Fatalf("newAgentClient: %v", err)
	}
	if err := client.login("load0001", "pw-123456"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := client.createCharacter("Load0001"); err != nil {
		t.Fatalf("createCharacter: %v", err)
	}
	if err := client.createCharacter("Taken"); err == nil || !strings.Contains(err.Error(), "native error code 67") {
		t.Fatalf("refused create error = %v, want native error code 67 named", err)
	}
	names, err := client.characterNames()
	if err != nil {
		t.Fatalf("characterNames: %v", err)
	}
	if len(names) != 1 || names[0] != "Load0001" {
		t.Fatalf("roster after creates = %v, want [Load0001]", names)
	}
}
