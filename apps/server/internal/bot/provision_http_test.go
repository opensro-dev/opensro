package bot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/bot"
)

func TestAgentProvisionerCreatesTheAccount(t *testing.T) {
	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != bot.PathAccounts || r.Method != http.MethodPost {
			http.Error(w, "route", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer provision-token" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		if body["id"] != "bot_0001" || body["password"] != "secret" {
			http.Error(w, "bad account", http.StatusBadRequest)
			return
		}
		sawAuth = true
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{"id": "bot_0001"})
	}))
	defer server.Close()

	provisioner := bot.AgentProvisioner{ProvisioningURL: server.URL, ProvisioningToken: "provision-token"}
	if err := provisioner.CreateAccount(context.Background(), "bot_0001", "secret"); err != nil {
		t.Fatal(err)
	}
	if !sawAuth {
		t.Fatal("the provisioning route was not reached")
	}
}

func TestAgentClientCreatesTheCharacter(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != bot.PathCharacterCreate {
			http.Error(w, "route", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer session-1" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"action": 1, "nativeResult": 1})
	}))
	defer server.Close()

	agent := bot.AgentClient{BaseURL: server.URL}
	if err := agent.CreateCharacter(context.Background(), "session-1", "global-official", "bot_0001", ""); err != nil {
		t.Fatal(err)
	}
	if sawBody["characterName"] != "bot_0001" || sawBody["divisionId"] != "global-official" {
		t.Fatalf("create body = %v", sawBody)
	}
	if sawBody["modelCodename"] != bot.DefaultModelCodename {
		t.Fatalf("model = %v, want %s", sawBody["modelCodename"], bot.DefaultModelCodename)
	}
	if sawBody["weaponSelected"] != true {
		t.Fatalf("weaponSelected = %v, want true", sawBody["weaponSelected"])
	}
}

// TestAgentProvisionerResetsAnExistingAccount guards the idempotent provision: a
// 409 on the create resets the password instead of failing, so a retry after a
// partial run finishes the bot.
func TestAgentProvisionerResetsAnExistingAccount(t *testing.T) {
	var passwordPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == bot.PathAccounts:
			w.WriteHeader(http.StatusConflict)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/password"):
			passwordPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	provisioner := bot.AgentProvisioner{ProvisioningURL: server.URL, ProvisioningToken: "provision-token"}
	if err := provisioner.CreateAccount(context.Background(), "bot_0012", "pw"); err != nil {
		t.Fatal(err)
	}
	if passwordPath != "/v1/accounts/bot_0012/password" {
		t.Fatalf("password path = %q", passwordPath)
	}
}
