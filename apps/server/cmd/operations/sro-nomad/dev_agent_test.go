package main

import (
	"path/filepath"
	"testing"

	nomad "github.com/hashicorp/nomad/api"
)

func TestValidateDevAgentIdentityAcceptsExactCheckout(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("C:", "checkout", "dev-windows.hcl")
	dataDir := filepath.Join("C:", "checkout", "dev-agent")
	self := compatibleDevAgentSelf(configPath, dataDir)

	err := validateDevAgentIdentity(self, devAgentExpectation{
		configPath: configPath,
		dataDir:    dataDir,
		oidcIssuer: developmentNomadAddress,
	})
	if err != nil {
		t.Fatalf("validate identity: %v", err)
	}
}

func TestValidateDevAgentIdentityRefusesAnotherDataDirectory(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("C:", "checkout", "dev-windows.hcl")
	self := compatibleDevAgentSelf(
		configPath,
		filepath.Join("C:", "other", "dev-agent"),
	)

	err := validateDevAgentIdentity(self, devAgentExpectation{
		configPath: configPath,
		dataDir:    filepath.Join("C:", "checkout", "dev-agent"),
		oidcIssuer: developmentNomadAddress,
	})
	if err == nil {
		t.Fatal("expected data directory mismatch")
	}
}

func TestValidateDevAgentIdentityRefusesStaleWorkloadIssuer(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("C:", "checkout", "dev-windows.hcl")
	dataDir := filepath.Join("C:", "checkout", "dev-agent")
	self := compatibleDevAgentSelf(configPath, dataDir)
	self.Config["Server"].(map[string]interface{})["OIDCIssuer"] = ""

	err := validateDevAgentIdentity(self, devAgentExpectation{
		configPath: configPath,
		dataDir:    dataDir,
		oidcIssuer: developmentNomadAddress,
	})
	if err == nil {
		t.Fatal("expected stale workload issuer refusal")
	}
}

func TestValidateDevAgentIdentityRefusesACLEnabledAgent(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("C:", "checkout", "dev-windows.hcl")
	dataDir := filepath.Join("C:", "checkout", "dev-agent")
	self := compatibleDevAgentSelf(configPath, dataDir)
	self.Config["ACL"] = map[string]interface{}{"Enabled": true}

	err := validateDevAgentIdentity(self, devAgentExpectation{
		configPath: configPath,
		dataDir:    dataDir,
		oidcIssuer: developmentNomadAddress,
	})
	if err == nil {
		t.Fatal("expected ACL-enabled agent refusal")
	}
}

func TestValidateDevNodeRequiresPlacementContract(t *testing.T) {
	t.Parallel()

	node := &nomad.Node{
		Name: "development",
		Meta: map[string]string{
			"sro_agent":  "true",
			"sro_shards": "test,global-official",
		},
		Drivers: map[string]*nomad.DriverInfo{
			"raw_exec": {
				Detected: true,
				Healthy:  true,
			},
		},
		HostNetworks: map[string]*nomad.HostNetworkInfo{
			"loopback": {CIDR: "127.0.0.1/32"},
		},
	}

	err := validateDevNode(node, []string{"global-official", "test"})
	if err != nil {
		t.Fatalf("validate node: %v", err)
	}

	node.Drivers["raw_exec"].Healthy = false
	if err := validateDevNode(
		node,
		[]string{"global-official", "test"},
	); err == nil {
		t.Fatal("expected unhealthy raw_exec refusal")
	}
}

func TestValidateNomadVersionOutputRequiresPinnedVersion(t *testing.T) {
	t.Parallel()

	if err := validateNomadVersionOutput([]byte(
		"Nomad v2.0.7\nBuildDate 2026-09-17T17:24:03Z\n",
	)); err != nil {
		t.Fatalf("validate pinned version: %v", err)
	}
	if err := validateNomadVersionOutput([]byte(
		"Nomad v2.0.4\n",
	)); err == nil {
		t.Fatal("expected unpinned version refusal")
	}
}

func compatibleDevAgentSelf(configPath string, dataDir string) *nomad.AgentSelf {
	return &nomad.AgentSelf{Config: map[string]interface{}{
		"DevMode":     true,
		"BindAddr":    "127.0.0.1",
		"DataDir":     dataDir,
		"ConfigPaths": configPath,
		"Server": map[string]interface{}{
			"Enabled":    true,
			"OIDCIssuer": developmentNomadAddress,
		},
		"Client": map[string]interface{}{
			"Enabled": true,
		},
		"Version": map[string]interface{}{
			"Version": developmentNomadVersion,
		},
	}}
}
