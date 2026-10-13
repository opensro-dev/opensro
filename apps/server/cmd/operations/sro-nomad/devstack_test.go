/*
===========================================================================

devstack_test.go - SRO_DEV_PORT_OFFSET: today's stack by default, a whole
second stack when set

===========================================================================
*/
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

/*
================
renderDevStack

The ports a loopback deployment and a dev-agent command take under the
current environment.
================
*/
func renderDevStack(t *testing.T) (deployment *deployment, agent map[string]any, args []string, options devAgentOptions) {
	t.Helper()
	command, err := parseOptions("deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err = resolveDeployment(command, false)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	options, err = parseDevAgentOptions([]string{"-nomad-binary", filepath.Join(dir, "nomad.exe"), "-data-dir", filepath.Join(dir, "dev-agent")})
	if err != nil {
		t.Fatal(err)
	}
	return deployment, deployment.agentVariables(), devAgentArgs(options), options
}

/*
================
TestDefaultDevStackIsTodaysStack

Unset and 0 keep every address the single local stack has always used, and
the dev agent's command line takes no extra config.
================
*/
func TestDefaultDevStackIsTodaysStack(t *testing.T) {
	for _, value := range []string{"", "0"} {
		t.Setenv(EnvDevPortOffset, value)
		deployment, agent, args, options := renderDevStack(t)
		if got := options.stack.nomadAddress(); got != "http://127.0.0.1:4646" {
			t.Fatalf("%q: Nomad address %s", value, got)
		}
		if agent["agent_port"] != 8787 || agent["agent_provisioning_port"] != 8789 {
			t.Fatalf("%q: Agent ports %v / %v", value, agent["agent_port"], agent["agent_provisioning_port"])
		}
		if got := deployment.Shards[0].PublicAPIAddr; got != "127.0.0.1:8790" {
			t.Fatalf("%q: public API %s", value, got)
		}
		want := []string{"agent", "-dev", "-log-level=INFO", "-config=" + options.config, "-data-dir=" + options.dataDir}
		if options.platformConfig != "" {
			want = append(want, "-config="+options.platformConfig)
		}
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Fatalf("%q: dev agent args %v, want %v", value, args, want)
		}
	}
}

/*
================
TestDevPortOffsetMovesTheWholeStack

An offset of 100 moves Nomad (HTTP, RPC, Serf and the identity issuer), the
Agent, its provisioning API, the public API and the client origins together, and the checkout's
credential exemption follows the moved agent and no other listener.
================
*/
func TestDevPortOffsetMovesTheWholeStack(t *testing.T) {
	t.Setenv(EnvDevPortOffset, "100")
	deployment, agent, args, options := renderDevStack(t)
	if got := options.stack.nomadAddress(); got != "http://127.0.0.1:4746" {
		t.Fatalf("Nomad address %s", got)
	}
	if agent["agent_port"] != 8887 || agent["agent_provisioning_port"] != 8889 {
		t.Fatalf("Agent ports %v / %v", agent["agent_port"], agent["agent_provisioning_port"])
	}
	if got := deployment.Shards[0].PublicAPIAddr; got != "127.0.0.1:8890" {
		t.Fatalf("public API %s", got)
	}
	origins := options.stack.allowedOrigins()
	if !strings.HasPrefix(origins, "http://127.0.0.1:5280,http://localhost:5280,http://127.0.0.1:4280,") ||
		strings.Contains(origins, ":5180") {
		t.Fatalf("moved origins %s", origins)
	}
	if got, err := normalizeAllowedOrigins(origins, "loopback"); err != nil || got == "" {
		t.Fatalf("moved origins refused: %v", err)
	}
	ports := options.stack.portsConfig()
	for _, line := range []string{"http = 4746", "rpc  = 4747", "serf = 4748", `oidc_issuer = "http://127.0.0.1:4746"`} {
		if !strings.Contains(ports, line) {
			t.Fatalf("ports config lacks %q:\n%s", line, ports)
		}
	}
	if last := args[len(args)-1]; last != "-config="+filepath.Join(filepath.Dir(options.dataDir), "dev-agent-ports.hcl") {
		t.Fatalf("the ports config is not merged last: %v", args)
	}

	expectation := devAgentExpectation{configPath: options.config, dataDir: options.dataDir, oidcIssuer: options.stack.nomadAddress()}
	self := compatibleDevAgentSelf(options.config, options.dataDir)
	self.Config["Server"].(map[string]any)["OIDCIssuer"] = options.stack.nomadAddress()
	if err := validateCheckoutDevelopmentAgent("http://127.0.0.1:4746", self, expectation); err != nil {
		t.Fatalf("the moved development agent was refused: %v", err)
	}
	if err := validateCheckoutDevelopmentAgent("http://127.0.0.1:4646", self, expectation); err == nil {
		t.Fatal("the default stack's listener was accepted for the moved stack")
	}
}

/*
================
TestDevPortOffsetRefusesBadValues
================
*/
func TestDevPortOffsetRefusesBadValues(t *testing.T) {
	for _, value := range []string{"-1", "ten", "1.5", "50001"} {
		_, err := loadDevStack(func(string) string { return value })
		if err == nil || !strings.Contains(err.Error(), EnvDevPortOffset) {
			t.Errorf("%q: err = %v, want a refusal naming %s", value, err, EnvDevPortOffset)
		}
	}
}
