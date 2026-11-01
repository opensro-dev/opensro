/*
===========================================================================

variables_staff_test.go - the GameWorld's staff webhook item (#1447)

The ops-only staff webhook travels as the GameWorld variable's
staff_discord_webhook item. A release must be able to set, change or keep
it while the GameWorld runs (its template is change_mode noop), and every
other GameWorld credential change must still demand a stop. The plan is
read against a fake Nomad that serves the stored variables and reports
every job as registered.

===========================================================================
*/
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/cluster/shard"
)

const (
	storedStaffWebhook = "https://discord.com/api/webhooks/123456789012345678/storedstafftoken0123456789"
	namedStaffWebhook  = "https://discord.com/api/webhooks/223456789012345678/namedstafftoken01234567890"
	staffTestRing      = "public-ring"
)

/*
================
staffPlanFixture

A deployment of one shard and a fake Nomad holding the Agent variable as
the deployment wants it and the GameWorld variable with gameWorldItems.
================
*/
func staffPlanFixture(t *testing.T, gameWorldItems nomad.VariableItems) (*deployment, *nomadClient) {
	t.Helper()
	deployment := &deployment{
		Shards:  []shardDeployment{{Definition: shard.Definition{ID: "global-official"}}},
		Secrets: clusterSecrets{SessionPublic: staffTestRing, SessionPrivate: "private-ring", ProvisioningToken: "provision"},
	}
	stored := map[string]nomad.VariableItems{
		agentVariablePath: {"agent_session_keyring": "private-ring", "agent_provisioning_token": "provision"},
		gameWorldVariablePrefix + gameWorldJobPrefix + "global-official" + gameWorldVariableSuffix: gameWorldItems,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasPrefix(request.URL.Path, "/v1/var/"):
			path := strings.TrimPrefix(request.URL.Path, "/v1/var/")
			items, found := stored[path]
			if !found {
				http.NotFound(response, request)
				return
			}
			_ = json.NewEncoder(response).Encode(nomad.Variable{Path: path, Items: items})
		case request.URL.Path == "/v1/vars":
			_, _ = response.Write([]byte("[]"))
		case strings.HasPrefix(request.URL.Path, "/v1/job/"):
			_ = json.NewEncoder(response).Encode(nomad.Job{ID: new(strings.TrimPrefix(request.URL.Path, "/v1/job/"))})
		default:
			http.Error(response, "unexpected test path "+request.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	config := nomad.DefaultConfig()
	config.Address = server.URL
	config.HttpClient = server.Client()
	api, err := nomad.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return deployment, &nomadClient{api: api}
}

/*
================
gameWorldPlanned
================
*/
func gameWorldPlanned(plan variablePlan) nomad.VariableItems {
	for _, variable := range plan.desired {
		if _, gameWorld := managedGameWorldVariableJobID(variable.path); gameWorld {
			return variable.items
		}
	}
	return nil
}

/*
================
TestAStaffWebhookChangeAloneNeedsNoGameWorldStop
================
*/
func TestAStaffWebhookChangeAloneNeedsNoGameWorldStop(t *testing.T) {
	// The first release that names it: the stored variable has no item yet.
	deployment, client := staffPlanFixture(t, nomad.VariableItems{"agent_session_public_keys": staffTestRing})
	deployment.StaffWebhook = namedStaffWebhook
	plan, err := deployment.planVariables(context.Background(), client, false)
	if err != nil {
		t.Fatalf("adding the staff webhook to a running GameWorld refused the plan: %v", err)
	}
	if len(plan.requiresStop) != 0 || gameWorldPlanned(plan)[staffWebhookItem] != namedStaffWebhook {
		t.Fatalf("stops %v, items %v; want no stop and the named webhook", plan.requiresStop, gameWorldPlanned(plan))
	}
}

/*
================
TestADeployWithoutAStaffWebhookKeepsTheStoredOne
================
*/
func TestADeployWithoutAStaffWebhookKeepsTheStoredOne(t *testing.T) {
	stored := nomad.VariableItems{"agent_session_public_keys": staffTestRing, staffWebhookItem: storedStaffWebhook}
	deployment, client := staffPlanFixture(t, stored)
	plan, err := deployment.planVariables(context.Background(), client, false)
	if err != nil || gameWorldPlanned(plan)[staffWebhookItem] != storedStaffWebhook {
		t.Fatalf("plan = %v, items %v; want the stored webhook kept without a stop", err, gameWorldPlanned(plan))
	}
	deployment, client = staffPlanFixture(t, stored)
	deployment.StaffWebhookOff = true
	plan, err = deployment.planVariables(context.Background(), client, false)
	if _, kept := gameWorldPlanned(plan)[staffWebhookItem]; err != nil || kept {
		t.Fatalf("off = %v, kept %t; want the webhook removed without a stop", err, kept)
	}
}

/*
================
TestOtherGameWorldCredentialsStillDemandAStop
================
*/
func TestOtherGameWorldCredentialsStillDemandAStop(t *testing.T) {
	deployment, client := staffPlanFixture(t, nomad.VariableItems{"agent_session_public_keys": "rotated-ring"})
	deployment.StaffWebhook = namedStaffWebhook
	_, err := deployment.planVariables(context.Background(), client, false)
	if err == nil || !strings.Contains(err.Error(), "differs while job") {
		t.Fatalf("a rotated verifier ring with a running GameWorld = %v, want the stop refusal", err)
	}
}

/*
================
TestConfiguredStaffWebhook
================
*/
func TestConfiguredStaffWebhook(t *testing.T) {
	if webhook, off, warning := configuredStaffWebhook(""); webhook != "" || off || warning != "" {
		t.Fatalf("unset = %q %t %q", webhook, off, warning)
	}
	if webhook, off, warning := configuredStaffWebhook("OFF"); webhook != "" || !off || warning != "" {
		t.Fatalf("off = %q %t %q", webhook, off, warning)
	}
	if webhook, _, warning := configuredStaffWebhook(" " + namedStaffWebhook + "\n"); webhook != namedStaffWebhook || warning != "" {
		t.Fatalf("a webhook = %q %q", webhook, warning)
	}
	// A wrong value never blocks the release: it is warned about, treated
	// as unset (the stored webhook stays), and never quoted.
	const wrong = "https://example.test/hook?token=leak"
	webhook, off, warning := configuredStaffWebhook(wrong)
	if webhook != "" || off || warning == "" || strings.Contains(warning, "leak") {
		t.Fatalf("a non-Discord URL = %q %t %q, want a warning that keeps the stored one", webhook, off, warning)
	}
}
