package bot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/releaseprotocol"
)

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestAgentClientRunsTheLoginFlow(t *testing.T) {
	var sawLogin, sawTransport, sawEnter bool
	mux := http.NewServeMux()
	mux.HandleFunc(bot.PathLogin, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(releaseprotocol.Header) != strconv.Itoa(releaseprotocol.Current) {
			http.Error(w, "protocol", http.StatusUpgradeRequired)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		if body["id"] != "bot_0001" || body["password"] != "secret" {
			http.Error(w, "bad login", http.StatusBadRequest)
			return
		}
		sawLogin = true
		writeJSON(w, map[string]any{
			"ok": true, "sessionToken": "session-1",
			"divisionId": "global-official", "transportUrl": "/shards/global-official",
		})
	})
	mux.HandleFunc(bot.PathTransportToken, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-1" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		sawTransport = true
		writeJSON(w, map[string]any{"ok": true, "token": "admission-1"})
	})
	mux.HandleFunc(bot.PathEnterWorldToken, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-1" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		if body["characterName"] != "bot_0001" || body["divisionId"] != "global-official" {
			http.Error(w, "bad character", http.StatusBadRequest)
			return
		}
		sawEnter = true
		writeJSON(w, map[string]any{"ok": true, "token": "enterworld-1"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	agent := bot.AgentClient{BaseURL: server.URL}
	login, err := agent.Login(ctx, "bot_0001", "secret", "global-official", "global-official")
	if err != nil {
		t.Fatal(err)
	}
	if login.SessionToken != "session-1" || login.DivisionID != "global-official" {
		t.Fatalf("login = %+v", login)
	}
	admission, err := agent.TransportToken(ctx, login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if admission != "admission-1" {
		t.Fatalf("admission token = %q", admission)
	}
	enterWorld, err := agent.EnterWorldToken(ctx, login.SessionToken, login.DivisionID, "bot_0001")
	if err != nil {
		t.Fatal(err)
	}
	if enterWorld != "enterworld-1" {
		t.Fatalf("enter-world token = %q", enterWorld)
	}
	if !sawLogin || !sawTransport || !sawEnter {
		t.Fatalf("routes hit = login %v transport %v enterworld %v", sawLogin, sawTransport, sawEnter)
	}
}

func TestAgentClientReportsARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "code": "INVALID_CREDENTIALS"})
	}))
	defer server.Close()
	agent := bot.AgentClient{BaseURL: server.URL}
	if _, err := agent.Login(context.Background(), "bot_0001", "wrong", "global-official", "global-official"); err == nil {
		t.Fatal("a refused login was accepted")
	}
}
