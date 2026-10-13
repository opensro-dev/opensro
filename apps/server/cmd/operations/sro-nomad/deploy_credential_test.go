package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

func TestValidateCheckoutDevelopmentAgentRequiresExactListenerIdentity(
	t *testing.T,
) {
	t.Parallel()

	configPath := filepath.Join("C:", "checkout", "dev-windows.hcl")
	dataDir := filepath.Join("C:", "checkout", "dev-agent")
	expectation := devAgentExpectation{
		configPath: configPath,
		dataDir:    dataDir,
		oidcIssuer: devStack{}.nomadAddress(),
	}
	self := compatibleDevAgentSelf(configPath, dataDir)

	if err := validateCheckoutDevelopmentAgent(
		devStack{}.nomadAddress()+"/",
		self,
		expectation,
	); err != nil {
		t.Fatalf("exact development listener: %v", err)
	}
	if err := validateCheckoutDevelopmentAgent(
		"http://127.0.0.1:14646",
		self,
		expectation,
	); err == nil {
		t.Fatal("forwarded or alternate Nomad listener was accepted")
	}
	other := compatibleDevAgentSelf(
		configPath,
		filepath.Join("C:", "other", "dev-agent"),
	)
	if err := validateCheckoutDevelopmentAgent(
		devStack{}.nomadAddress(),
		other,
		expectation,
	); err == nil {
		t.Fatal("another checkout's development agent was accepted")
	}
}

func TestValidateProductionDeployCredentialAcceptsBoundedLocalClient(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	token := &nomad.ACLToken{
		Type:           nomadClientTokenType,
		Policies:       []string{productionDeployPolicy},
		CreateTime:     now,
		ExpirationTime: &expires,
	}
	if err := validateProductionDeployCredential(token, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProductionDeployCredentialRefusesBroadOrStaleTokens(
	t *testing.T,
) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	valid := func() *nomad.ACLToken {
		expires := now.Add(time.Hour)
		return &nomad.ACLToken{
			Type:           nomadClientTokenType,
			Policies:       []string{productionDeployPolicy},
			CreateTime:     now,
			ExpirationTime: &expires,
		}
	}
	tests := []struct {
		name    string
		mutate  func(*nomad.ACLToken)
		wantErr string
	}{
		{
			"management",
			func(token *nomad.ACLToken) { token.Type = "management" },
			"client token",
		},
		{
			"global",
			func(token *nomad.ACLToken) { token.Global = true },
			"region-local",
		},
		{
			"role",
			func(token *nomad.ACLToken) {
				token.Roles = []*nomad.ACLTokenRoleLink{{Name: "deployer"}}
			},
			"ACL roles",
		},
		{
			"missing policy",
			func(token *nomad.ACLToken) { token.Policies = nil },
			"exactly policy",
		},
		{
			"extra policy",
			func(token *nomad.ACLToken) {
				token.Policies = append(token.Policies, "unrelated")
			},
			"exactly policy",
		},
		{
			"permanent",
			func(token *nomad.ACLToken) { token.ExpirationTime = nil },
			"does not expire",
		},
		{
			"overlong",
			func(token *nomad.ACLToken) {
				expires := token.CreateTime.Add(maximumDeployTokenLifetime + time.Second)
				token.ExpirationTime = &expires
			},
			"maximum",
		},
		{
			"near expiry",
			func(token *nomad.ACLToken) {
				expires := now.Add(minimumDeployTokenRemaining - time.Second)
				token.ExpirationTime = &expires
			},
			"at least",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := valid()
			test.mutate(token)
			err := validateProductionDeployCredential(token, now)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
	if err := validateProductionDeployCredential(nil, now); err == nil {
		t.Fatal("nil token was accepted")
	}
}
