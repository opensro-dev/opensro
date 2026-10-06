/*
===========================================================================

provisioning_port_test.go - the Agent's provisioning port reaches its job

===========================================================================
*/

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"opensro.online/server/internal/agent/provisioning"
)

// wantDefaultProvisioningPort pins the value the web site's provisioning
// client calls; provisioning.DefaultPort is its only definition.
const wantDefaultProvisioningPort = 8789

/*
================
TestAgentProvisioningPortDefaultsAndOverrides

Unset, the Agent job gets the documented loopback default 8789 (the web
site calls it there); a second Agent on one host passes its own port; a
port equal to the Agent HTTP port is refused.
================
*/
func TestAgentProvisioningPortDefaultsAndOverrides(t *testing.T) {
	render := func(arguments ...string) (map[string]any, error) {
		options, err := parseOptions("deploy", arguments)
		if err != nil {
			return nil, err
		}
		deployment, err := resolveDeployment(options, false)
		if err != nil {
			return nil, err
		}
		return deployment.agentVariables(), nil
	}
	variables, err := render()
	if err != nil {
		t.Fatal(err)
	}
	if variables["agent_provisioning_port"] != wantDefaultProvisioningPort || provisioning.DefaultPort != wantDefaultProvisioningPort {
		t.Fatalf("default provisioning port = %v, want 8789", variables["agent_provisioning_port"])
	}
	variables, err = render("-agent-port", "8817", "-agent-provisioning-port", "8819")
	if err != nil {
		t.Fatal(err)
	}
	if variables["agent_provisioning_port"] != 8819 || variables["agent_port"] != 8817 {
		t.Fatalf("overridden ports = %v / %v", variables["agent_port"], variables["agent_provisioning_port"])
	}
	if _, err := render("-agent-port", "8817", "-agent-provisioning-port", "8817"); err == nil {
		t.Fatal("a provisioning port equal to the Agent HTTP port was accepted")
	}
}

/*
================
TestAgentJobProvisioningDefaultMatchesTheOwner

The job file's HCL default only serves a manual 'nomad job run' without
sro-nomad, but it must still equal provisioning.DefaultPort.
================
*/
func TestAgentJobProvisioningDefaultMatchesTheOwner(t *testing.T) {
	options, err := parseOptions("deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := resolveDeployment(options, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := os.ReadFile(filepath.Join(deployment.JobsDir, agentTemplateName))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`variable "agent_provisioning_port" \{[^}]*default\s*=\s*(\d+)`).FindSubmatch(job)
	if match == nil {
		t.Fatal("agent job has no agent_provisioning_port default")
	}
	if port, _ := strconv.Atoi(string(match[1])); port != provisioning.DefaultPort {
		t.Fatalf("agent job default %d, provisioning.DefaultPort %d", port, provisioning.DefaultPort)
	}
}
