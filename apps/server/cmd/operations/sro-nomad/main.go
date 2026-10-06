/*
===========================================================================

main.go - the Nomad command entry point

Parses operator choices and dispatches fleet operations. Nomad owns process
supervision and recovery.

===========================================================================
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

/*
================
main
================
*/
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	if os.Args[1] == "dev-agent" {
		if err := runDevAgent(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sro-nomad dev-agent: %v\n", err)
			os.Exit(1)
		}
		return
	}
	// Browser releases are files behind the edge, not Nomad jobs.
	if os.Args[1] == "publish-client" {
		if err := runPublishClient(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sro-nomad publish-client: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	var err error
	switch os.Args[1] {
	case "validate":
		err = runValidate(ctx, os.Args[2:])
	case "deploy":
		err = runDeploy(ctx, os.Args[2:])
	case "stop":
		err = runStop(ctx, os.Args[2:])
	case "status":
		err = runStatus(ctx, os.Args[2:])
	case "notice":
		err = runNotice(ctx, os.Args[2:])
	case "rotate-session-key":
		err = runRotateSessionKey(ctx, os.Args[2:])
	case "maintenance-list":
		err = runMaintenanceList(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "sro-nomad %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

/*
================
runValidate
================
*/
func runValidate(ctx context.Context, arguments []string) error {
	options, err := parseOptions("validate", arguments)
	if err != nil {
		return err
	}
	deployment, err := resolveDeployment(options, true)
	if err != nil {
		return err
	}
	client, err := newNomadClient(options.Namespace)
	if err != nil {
		return err
	}
	defer client.close()
	if err := requireDeploymentCredential(ctx, client, options); err != nil {
		return err
	}
	if err := deployment.validateJobs(ctx, client); err != nil {
		return err
	}
	return deployment.validateVariables(ctx, client)
}

/*
================
runDeploy
================
*/
func runDeploy(ctx context.Context, arguments []string) error {
	options, err := parseOptions("deploy", arguments)
	if err != nil {
		return err
	}
	client, err := newNomadClient(options.Namespace)
	if err != nil {
		return err
	}
	defer client.close()
	if err := requireDeploymentCredential(ctx, client, options); err != nil {
		return err
	}
	return client.withFleetLock(
		ctx,
		"deploy",
		func(lockContext context.Context) error {
			if options.Build {
				buildDeployment, err := resolveDeployment(options, false)
				if err != nil {
					return err
				}
				if err := buildDeployment.buildBinaries(lockContext); err != nil {
					return err
				}
			}
			deployment, err := resolveDeployment(options, true)
			if err != nil {
				return err
			}
			if err := deployment.stageReleases(); err != nil {
				return err
			}
			if err := deployment.validateJobs(lockContext, client); err != nil {
				return err
			}
			if err := deployment.putVariables(lockContext, client); err != nil {
				return err
			}
			if err := deployment.deploy(lockContext, client); err != nil {
				return err
			}
			if err := client.pruneReleases(lockContext, deployment); err != nil {
				return err
			}
			fmt.Printf(
				"Nomad reconciled Agent and %d enabled GameWorld shard(s)\n",
				len(deployment.Shards),
			)
			return nil
		},
	)
}

/*
================
runStop
================
*/
func runStop(ctx context.Context, arguments []string) error {
	options, err := parseOptions("stop", arguments)
	if err != nil {
		return err
	}
	deployment, err := resolveDeployment(options, false)
	if err != nil {
		return err
	}
	client, err := newNomadClient(options.Namespace)
	if err != nil {
		return err
	}
	defer client.close()
	if err := requireDeploymentCredential(ctx, client, options); err != nil {
		return err
	}
	return client.withFleetLock(
		ctx,
		"stop",
		func(lockContext context.Context) error {
			return deployment.stop(lockContext, client)
		},
	)
}

/*
================
runStatus
================
*/
func runStatus(ctx context.Context, arguments []string) error {
	options, err := parseOptions("status", arguments)
	if err != nil {
		return err
	}
	deployment, err := resolveDeployment(options, false)
	if err != nil {
		return err
	}
	client, err := newNomadClient(options.Namespace)
	if err != nil {
		return err
	}
	defer client.close()
	return deployment.status(ctx, client)
}

/*
================
commandOptions
================
*/
type commandOptions struct {
	ModuleRoot      string
	Catalog         string
	StateDir        string
	ReleaseDir      string
	JobsDir         string
	Namespace       string
	Network         string
	AllowedOrigins  string
	GMCharacters    string
	TransportCert   string
	TransportKey    string
	IdentityIssuer  string
	IdentityJWKSURL string
	AgentURL        string
	AgentPort       int
	// AgentProvisioningPort is the Agent's loopback account provisioning port.
	AgentProvisioningPort int
	PrivateNet            bool
	Build                 bool
	Pprof                 bool
	TaskUser              string
	AgentCPU              int
	AgentMemoryMB         int
	GameCPU               int
	GameMemoryMB          int
}

/*
================
parseOptions
================
*/
func parseOptions(name string, arguments []string) (commandOptions, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var options commandOptions
	flags.StringVar(&options.ModuleRoot, "module-root", "", "server module root (default: auto-detect)")
	flags.StringVar(&options.Catalog, "catalog", "", "shard catalog (default: <module>/config/shards.json)")
	flags.StringVar(&options.StateDir, "state-dir", "", "cluster credential state (default: <module>/.state/cluster)")
	flags.StringVar(&options.ReleaseDir, "release-dir", "", "immutable release root (default: <state-dir>/releases)")
	flags.StringVar(&options.JobsDir, "jobs-dir", "", "Nomad job templates (default: <module>/ops/nomad/jobs)")
	flags.StringVar(
		&options.Namespace,
		"namespace",
		"",
		"Nomad namespace (default: NOMAD_NAMESPACE or default)",
	)
	flags.StringVar(
		&options.IdentityIssuer,
		"identity-issuer",
		"",
		"stable Nomad OIDC issuer reachable by Agent",
	)
	flags.StringVar(
		&options.IdentityJWKSURL,
		"identity-jwks-url",
		"",
		"Nomad workload JWKS URL (default: <identity-issuer>/.well-known/jwks.json)",
	)
	flags.StringVar(
		&options.AgentURL,
		"agent-url",
		"",
		"Agent URL reachable by the deployer (default: loopback Agent port)",
	)
	flags.StringVar(&options.Network, "host-network", "loopback", "Nomad client host_network name")
	flags.StringVar(
		&options.AllowedOrigins,
		"allowed-origins",
		"",
		"comma-separated exact browser origins (required outside loopback)",
	)
	flags.StringVar(
		&options.GMCharacters,
		"gm-characters",
		strings.TrimSpace(os.Getenv("SRO_GM_CHARACTERS")),
		"comma-separated division:character GM allowlist (default: SRO_GM_CHARACTERS)",
	)
	flags.StringVar(
		&options.TransportCert,
		"transport-cert-file",
		"",
		"production WebTransport TLS certificate readable on every GameWorld node",
	)
	flags.StringVar(
		&options.TransportKey,
		"transport-key-file",
		"",
		"production WebTransport TLS private key readable on every GameWorld node",
	)
	flags.IntVar(&options.AgentPort, "agent-port", 8787, "Agent HTTP port")
	flags.IntVar(
		&options.AgentProvisioningPort,
		"agent-provisioning-port",
		defaultAgentProvisioningPort,
		"Agent loopback account provisioning port (a second Agent on one host needs its own)",
	)
	flags.BoolVar(
		&options.PrivateNet,
		"private-network",
		false,
		"acknowledge that a non-loopback host network is private behind TLS ingress",
	)
	flags.BoolVar(&options.Build, "build", false, "build the agent and gameworld binaries before deploy")
	flags.BoolVar(
		&options.Pprof,
		"pprof",
		false,
		"serve /debug/pprof/ on the GameWorld loopback control listener (loopback host network only)",
	)
	flags.StringVar(&options.TaskUser, "task-user", "", "OS account the services run as (Linux: a dedicated unprivileged user)")
	flags.IntVar(&options.AgentCPU, "agent-cpu", 0, "Agent CPU reservation in MHz (default: the job's)")
	flags.IntVar(&options.AgentMemoryMB, "agent-memory-mb", 0, "Agent memory reservation in MB (default: the job's)")
	flags.IntVar(&options.GameCPU, "gameworld-cpu", 0, "GameWorld CPU reservation in MHz (default: the job's)")
	flags.IntVar(&options.GameMemoryMB, "gameworld-memory-mb", 0, "GameWorld memory reservation in MB (default: the job's)")
	if err := flags.Parse(arguments); err != nil {
		return commandOptions{}, err
	}
	if flags.NArg() != 0 {
		return commandOptions{}, fmt.Errorf("positional arguments are not accepted")
	}
	namespace, err := configuredNomadNamespace(options.Namespace)
	if err != nil {
		return commandOptions{}, err
	}
	options.Namespace = namespace
	return options, nil
}

/*
================
configuredNomadNamespace
================
*/
func configuredNomadNamespace(configured string) (string, error) {
	namespace := strings.TrimSpace(configured)
	if namespace == "" {
		namespace = strings.TrimSpace(os.Getenv("NOMAD_NAMESPACE"))
	}
	if namespace == "" {
		namespace = defaultNomadNamespace
	}
	if len(namespace) > 64 {
		return "", fmt.Errorf(
			"nomad namespace %q is longer than 64 bytes",
			namespace,
		)
	}
	for _, character := range namespace {
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			character == '-' {
			continue
		}
		return "", fmt.Errorf(
			"nomad namespace %q must use lowercase letters, digits, and hyphens",
			namespace,
		)
	}
	return namespace, nil
}

/*
================
usage
================
*/
func usage() {
	fmt.Fprintln(
		os.Stderr,
		"usage: sro-nomad <dev-agent|validate|deploy|stop|status|notice|rotate-session-key|maintenance-list> [options]",
	)
}
