package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/config"
)

const (
	developmentNomadAddress = "http://127.0.0.1:4646"
	developmentNomadVersion = "2.0.7"
	devAgentProbeTimeout    = 2 * time.Second
	devAgentReadyTimeout    = 15 * time.Second
)

type devAgentOptions struct {
	moduleRoot string
	binary     string
	config     string
	dataDir    string
}

type devAgentExpectation struct {
	configPath string
	dataDir    string
	oidcIssuer string
	shards     []string
}

// runDevAgent is an idempotent development entry point. Nomad remains the
// process owner: this command only validates/reuses a compatible local agent
// or starts the pinned agent in the foreground when none is listening.
func runDevAgent(arguments []string) error {
	options, err := parseDevAgentOptions(arguments)
	if err != nil {
		return err
	}
	expectation, err := resolveDevAgentExpectation(options)
	if err != nil {
		return err
	}

	present, err := reuseCompatibleDevAgent(
		developmentNomadAddress,
		expectation,
	)
	if err != nil {
		return err
	}
	if present {
		fmt.Printf(
			"Compatible Nomad %s development agent is already ready at %s; reusing it\n",
			developmentNomadVersion,
			developmentNomadAddress,
		)
		return nil
	}
	if err := prepareDevAgentStart(options); err != nil {
		return err
	}

	fmt.Printf(
		"Starting Nomad %s development agent at %s\n",
		developmentNomadVersion,
		developmentNomadAddress,
	)
	command := exec.Command(
		options.binary,
		"agent",
		"-dev",
		"-log-level=INFO",
		"-config="+expectation.configPath,
		"-data-dir="+expectation.dataDir,
	)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		// Two simultaneous callers can both observe an unused port. The winner
		// owns the agent; the loser succeeds only if that agent proves it has
		// the exact identity expected by this checkout.
		present, probeErr := reuseCompatibleDevAgent(
			developmentNomadAddress,
			expectation,
		)
		if probeErr == nil && present {
			fmt.Printf(
				"Compatible Nomad development agent won the startup race at %s; reusing it\n",
				developmentNomadAddress,
			)
			return nil
		}
		if probeErr != nil {
			return errors.Join(
				fmt.Errorf("nomad development agent exited: %w", err),
				probeErr,
			)
		}
		return fmt.Errorf("nomad development agent exited: %w", err)
	}
	return nil
}

func parseDevAgentOptions(arguments []string) (devAgentOptions, error) {
	flags := flag.NewFlagSet("dev-agent", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var options devAgentOptions
	flags.StringVar(
		&options.moduleRoot,
		"module-root",
		"",
		"server module root (default: auto-detect)",
	)
	flags.StringVar(
		&options.binary,
		"nomad-binary",
		os.Getenv("SRO_NOMAD_BINARY"),
		"Nomad executable (default: workspace-pinned Nomad on Windows)",
	)
	flags.StringVar(
		&options.config,
		"config",
		"",
		"development agent config (default: <module>/ops/nomad/config/dev-windows.hcl)",
	)
	flags.StringVar(
		&options.dataDir,
		"data-dir",
		"",
		"development agent data (default: <module>/.state/nomad/dev-agent)",
	)
	if err := flags.Parse(arguments); err != nil {
		return devAgentOptions{}, err
	}
	if flags.NArg() != 0 {
		return devAgentOptions{}, fmt.Errorf(
			"positional arguments are not accepted",
		)
	}

	if options.moduleRoot == "" {
		var err error
		options.moduleRoot, err = config.FindModuleRoot()
		if err != nil {
			return devAgentOptions{}, err
		}
	}
	options.moduleRoot = cleanAbsolute(options.moduleRoot)
	if options.config == "" {
		options.config = filepath.Join(
			options.moduleRoot,
			"ops",
			"nomad",
			"config",
			"dev-windows.hcl",
		)
	}
	if options.dataDir == "" {
		options.dataDir = filepath.Join(
			options.moduleRoot,
			".state",
			"nomad",
			"dev-agent",
		)
	}
	if options.binary == "" {
		if runtime.GOOS == "windows" {
			options.binary = filepath.Join(
				options.moduleRoot,
				"..",
				"..",
				".tools",
				"nomad",
				developmentNomadVersion,
				"nomad.exe",
			)
		} else {
			var err error
			options.binary, err = exec.LookPath("nomad")
			if err != nil {
				return devAgentOptions{}, fmt.Errorf(
					"find Nomad executable: %w",
					err,
				)
			}
		}
	}
	options.binary = cleanAbsolute(options.binary)
	options.config = cleanAbsolute(options.config)
	options.dataDir = cleanAbsolute(options.dataDir)

	if err := requireRegularFile(options.config); err != nil {
		return devAgentOptions{}, err
	}
	return options, nil
}

func prepareDevAgentStart(options devAgentOptions) error {
	if err := requireRegularFile(options.binary); err != nil {
		return err
	}
	versionOutput, err := exec.Command(options.binary, "version").Output()
	if err != nil {
		return fmt.Errorf("read Nomad executable version: %w", err)
	}
	if err := validateNomadVersionOutput(versionOutput); err != nil {
		return err
	}
	if err := os.MkdirAll(options.dataDir, 0o700); err != nil {
		return fmt.Errorf(
			"create Nomad development data directory %s: %w",
			options.dataDir,
			err,
		)
	}
	return nil
}

func validateNomadVersionOutput(versionOutput []byte) error {
	versionLine := strings.SplitN(string(versionOutput), "\n", 2)[0]
	if strings.TrimSpace(versionLine) != "Nomad v"+developmentNomadVersion {
		return fmt.Errorf(
			"nomad executable reports %q, want %q",
			strings.TrimSpace(versionLine),
			"Nomad v"+developmentNomadVersion,
		)
	}
	return nil
}

func resolveDevAgentExpectation(
	options devAgentOptions,
) (devAgentExpectation, error) {
	catalog, err := shard.Load(filepath.Join(
		options.moduleRoot,
		shard.DefaultCatalogPath,
	))
	if err != nil {
		return devAgentExpectation{}, fmt.Errorf("shard catalog: %w", err)
	}
	shards := make([]string, 0)
	for _, definition := range catalog.Definitions() {
		if definition.Enabled {
			shards = append(shards, definition.ID)
		}
	}
	sort.Strings(shards)
	if len(shards) == 0 {
		return devAgentExpectation{}, fmt.Errorf(
			"shard catalog has no enabled shards",
		)
	}
	return devAgentExpectation{
		configPath: options.config,
		dataDir:    options.dataDir,
		oidcIssuer: developmentNomadAddress,
		shards:     shards,
	}, nil
}

func reuseCompatibleDevAgent(
	address string,
	expectation devAgentExpectation,
) (bool, error) {
	listening, err := endpointListening(address)
	if err != nil {
		return false, err
	}
	if !listening {
		return false, nil
	}

	client, err := newDevAgentAPIClient(address)
	if err != nil {
		return true, err
	}
	defer client.Close()

	deadline := time.Now().Add(devAgentReadyTimeout)
	var identityVerified bool
	var lastReadinessError error
	for {
		self, err := client.Agent().Self()
		if err != nil {
			lastReadinessError = fmt.Errorf(
				"read Nomad agent identity: %w",
				err,
			)
		} else {
			if err := validateDevAgentIdentity(self, expectation); err != nil {
				return true, fmt.Errorf(
					"listener at %s is not the development agent for this checkout: %w",
					address,
					err,
				)
			}
			identityVerified = true
			readyContext, cancel := context.WithTimeout(
				context.Background(),
				devAgentProbeTimeout,
			)
			lastReadinessError = validateDevAgentReadiness(
				readyContext,
				client,
				expectation,
			)
			cancel()
			if lastReadinessError == nil {
				return true, nil
			}
		}

		if time.Now().After(deadline) {
			if !identityVerified {
				return true, fmt.Errorf(
					"listener at %s did not expose the expected Nomad API within %s: %w",
					address,
					devAgentReadyTimeout,
					lastReadinessError,
				)
			}
			return true, fmt.Errorf(
				"compatible Nomad agent at %s did not become ready within %s: %w",
				address,
				devAgentReadyTimeout,
				lastReadinessError,
			)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func newDevAgentAPIClient(address string) (*nomad.Client, error) {
	apiConfig := nomad.DefaultConfig()
	apiConfig.Address = address
	apiConfig.HttpClient = &http.Client{Timeout: devAgentProbeTimeout}
	client, err := nomad.NewClient(apiConfig)
	if err != nil {
		return nil, fmt.Errorf("configure Nomad development API: %w", err)
	}
	return client, nil
}

func validateDevAgentIdentity(
	self *nomad.AgentSelf,
	expectation devAgentExpectation,
) error {
	if self == nil {
		return fmt.Errorf("agent identity response is empty")
	}
	if !configBool(self.Config, "DevMode") {
		return fmt.Errorf("agent is not running in development mode")
	}
	if !nestedConfigBool(self.Config, "Server", "Enabled") {
		return fmt.Errorf("server role is disabled")
	}
	if issuer := nestedConfigString(
		self.Config,
		"Server",
		"OIDCIssuer",
	); issuer != expectation.oidcIssuer {
		return fmt.Errorf(
			"workload identity issuer is %q, want %q; restart the development Agent to apply its current config",
			issuer,
			expectation.oidcIssuer,
		)
	}
	if !nestedConfigBool(self.Config, "Client", "Enabled") {
		return fmt.Errorf("client role is disabled")
	}
	if nestedConfigBool(self.Config, "ACL", "Enabled") {
		return fmt.Errorf(
			"ACLs are enabled; the credential-free development contract requires them to be disabled",
		)
	}
	if bindAddress := configString(self.Config, "BindAddr"); !isLoopbackHost(
		bindAddress,
	) {
		return fmt.Errorf("bind address %q is not loopback", bindAddress)
	}
	if dataDir := configString(self.Config, "DataDir"); !samePath(
		dataDir,
		expectation.dataDir,
	) {
		return fmt.Errorf(
			"data directory is %q, want %q",
			dataDir,
			expectation.dataDir,
		)
	}
	if !configContainsPath(
		self.Config["ConfigPaths"],
		expectation.configPath,
	) {
		return fmt.Errorf(
			"configuration does not include %q",
			expectation.configPath,
		)
	}
	version := nestedConfigString(self.Config, "Version", "Version")
	if version != developmentNomadVersion {
		return fmt.Errorf(
			"nomad version is %q, want %q",
			version,
			developmentNomadVersion,
		)
	}
	return nil
}

func validateDevAgentReadiness(
	ctx context.Context,
	client *nomad.Client,
	expectation devAgentExpectation,
) error {
	health, err := client.Agent().Health()
	if err != nil {
		return fmt.Errorf("read Nomad agent health: %w", err)
	}
	if health.Server == nil || !health.Server.Ok {
		return fmt.Errorf("nomad server role is not healthy")
	}
	if health.Client == nil || !health.Client.Ok {
		return fmt.Errorf("nomad client role is not healthy")
	}

	nodes, _, err := client.Nodes().List(
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("list Nomad development nodes: %w", err)
	}
	var reasons []string
	for _, summary := range nodes {
		if summary.Status != nomad.NodeStatusReady {
			continue
		}
		node, _, err := client.Nodes().Info(
			summary.ID,
			(&nomad.QueryOptions{}).WithContext(ctx),
		)
		if err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		if err := validateDevNode(node, expectation.shards); err == nil {
			return nil
		} else {
			reasons = append(reasons, err.Error())
		}
	}
	if len(reasons) == 0 {
		return fmt.Errorf("no ready Nomad client node is registered")
	}
	return fmt.Errorf(
		"no compatible ready Nomad client node: %s",
		strings.Join(reasons, "; "),
	)
}

func validateDevNode(node *nomad.Node, expectedShards []string) error {
	if node == nil {
		return fmt.Errorf("node response is empty")
	}
	if !strings.EqualFold(node.Meta["sro_agent"], "true") {
		return fmt.Errorf("node %q does not allow Agent placement", node.Name)
	}
	shards := strings.Split(node.Meta["sro_shards"], ",")
	for index := range shards {
		shards[index] = strings.TrimSpace(shards[index])
	}
	sort.Strings(shards)
	if strings.Join(shards, ",") != strings.Join(expectedShards, ",") {
		return fmt.Errorf(
			"node %q shard grant is %q, want %q",
			node.Name,
			node.Meta["sro_shards"],
			strings.Join(expectedShards, ","),
		)
	}
	driver := node.Drivers["raw_exec"]
	if driver == nil || !driver.Detected || !driver.Healthy {
		return fmt.Errorf("node %q raw_exec driver is not healthy", node.Name)
	}
	network := node.HostNetworks["loopback"]
	if network == nil || network.CIDR != "127.0.0.1/32" {
		return fmt.Errorf(
			"node %q loopback host network is missing",
			node.Name,
		)
	}
	return nil
}

func endpointListening(address string) (bool, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return false, fmt.Errorf("parse Nomad address: %w", err)
	}
	connection, err := net.DialTimeout(
		"tcp",
		parsed.Host,
		devAgentProbeTimeout,
	)
	if err != nil {
		return false, nil //nolint:nilerr // a refused dial is the answer "not listening", not a failure
	}
	if err := connection.Close(); err != nil {
		return true, fmt.Errorf("close Nomad probe connection: %w", err)
	}
	return true, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func configBool(values map[string]interface{}, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func configString(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return value
}

func nestedConfigBool(
	values map[string]interface{},
	outer string,
	inner string,
) bool {
	nested, _ := values[outer].(map[string]interface{})
	return configBool(nested, inner)
}

func nestedConfigString(
	values map[string]interface{},
	outer string,
	inner string,
) string {
	nested, _ := values[outer].(map[string]interface{})
	return configString(nested, inner)
}

func configContainsPath(value interface{}, expected string) bool {
	switch paths := value.(type) {
	case string:
		return samePath(paths, expected)
	case []interface{}:
		for _, candidate := range paths {
			path, _ := candidate.(string)
			if samePath(path, expected) {
				return true
			}
		}
	case []string:
		for _, path := range paths {
			if samePath(path, expected) {
				return true
			}
		}
	}
	return false
}

func samePath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
