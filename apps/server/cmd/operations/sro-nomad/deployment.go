/*
===========================================================================

deployment.go - deployment inputs and job variables

Resolves the catalog, identity, and game-data contracts before Nomad mutations.

===========================================================================
*/
package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/config"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/security/auth"
)

const (
	agentJobName        = "sro-agent"
	gameWorldJobPrefix  = "sro-gameworld-"
	gameWorldShardToken = "__SHARD_ID__"
	agentAccountToken   = "__ACCOUNT_VARIABLES__"
	agentTemplateName   = "agent.nomad.hcl"
	gameTemplateName    = "gameworld.nomad.hcl"
	accountChunkBytes   = 48 << 10
	maxAccountChunks    = int(
		(auth.MaxFileBytes + accountChunkBytes - 1) / accountChunkBytes,
	)
	developmentAllowedOrigins = "http://127.0.0.1:5180,http://localhost:5180,http://127.0.0.1:4180,http://localhost:4180," +
		"http://127.0.0.1:4173," +
		"http://127.0.0.1:5174," +
		"http://localhost:4173," +
		"http://localhost:5174"
	developmentGMCharacters = "global-official:asd2"
)

/*
================
deployment
================
*/
type deployment struct {
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
	TransportTLSID  string
	IdentityIssuer  string
	IdentityJWKSURL string
	AgentURL        string
	AgentPort       int
	PrivateNetwork  bool
	TaskUser        string
	TaskUID         int
	TaskGID         int
	AgentCPU        int
	AgentMemoryMB   int
	GameCPU         int
	GameMemoryMB    int
	AgentReleaseID  string
	GameReleaseID   string
	AgentSource     string
	GameSource      string
	AgentBinary     string
	GameBinary      string
	DataPaths       gamedata.Paths
	Shards          []shardDeployment
	Secrets         clusterSecrets
	// BugReports comes from the deployer's SRO_BUG_REPORT_* environment;
	// its webhook travels only as a Nomad variable item.
	BugReports bugreport.Config
}

/*
================
shardDeployment
================
*/
type shardDeployment struct {
	Definition    shard.Definition
	ControlPort   int
	TransportPort int
	AuthorityDir  string
}

/*
================
clusterSecrets
================
*/
type clusterSecrets struct {
	AccountChunks     []string
	SessionPrivate    string
	SessionPublic     string
	ProvisioningToken string
}

/*
================
resolveDeployment
================
*/
func resolveDeployment(
	options commandOptions,
	requirePrerequisites bool,
) (*deployment, error) {
	taskUID, taskGID, err := resolveTaskOwner(options.TaskUser)
	if err != nil {
		return nil, err
	}
	moduleRoot := options.ModuleRoot
	if moduleRoot == "" {
		var err error
		moduleRoot, err = config.FindModuleRoot()
		if err != nil {
			return nil, err
		}
	}
	moduleRoot = cleanAbsolute(moduleRoot)

	catalogPath := options.Catalog
	if catalogPath == "" {
		catalogPath = filepath.Join(moduleRoot, shard.DefaultCatalogPath)
	}
	stateDir := options.StateDir
	if stateDir == "" {
		stateDir = filepath.Join(moduleRoot, ".state", "cluster")
	}
	stateDir = cleanAbsolute(stateDir)
	releaseDir := options.ReleaseDir
	if releaseDir == "" {
		releaseDir = filepath.Join(stateDir, "releases")
	}
	releaseDir = cleanAbsolute(releaseDir)
	jobsDir := options.JobsDir
	if jobsDir == "" {
		jobsDir = filepath.Join(moduleRoot, "ops", "nomad", "jobs")
	}

	catalog, err := shard.Load(cleanAbsolute(catalogPath))
	if err != nil {
		return nil, fmt.Errorf("shard catalog: %w", err)
	}
	var dataPaths gamedata.Paths
	var secrets clusterSecrets
	var agentReleaseID string
	var gameReleaseID string
	if requirePrerequisites {
		dataPaths, err = gamedata.Resolve()
		if err != nil {
			return nil, fmt.Errorf("game data: %w", err)
		}
		secrets, err = loadClusterSecrets(cleanAbsolute(stateDir))
		if err != nil {
			return nil, err
		}
		for _, name := range []string{
			binaryName("agent"),
			binaryName("gameworld"),
		} {
			if err := requireRegularFile(filepath.Join(moduleRoot, name)); err != nil {
				return nil, err
			}
		}
		for _, name := range []string{
			agentTemplateName,
			gameTemplateName,
		} {
			if err := requireRegularFile(filepath.Join(jobsDir, name)); err != nil {
				return nil, err
			}
		}
		agentSource := filepath.Join(moduleRoot, binaryName("agent"))
		gameSource := filepath.Join(moduleRoot, binaryName("gameworld"))
		agentReleaseID, err = releaseID(
			agentSource,
			filepath.Join(jobsDir, agentTemplateName),
		)
		if err != nil {
			return nil, fmt.Errorf("agent release identity: %w", err)
		}
		gameReleaseID, err = releaseID(
			gameSource,
			filepath.Join(jobsDir, gameTemplateName),
		)
		if err != nil {
			return nil, fmt.Errorf("GameWorld release identity: %w", err)
		}
	}
	if options.AgentPort < 1 || options.AgentPort > 65535 {
		return nil, fmt.Errorf("agent port %d is invalid", options.AgentPort)
	}
	hostNetwork, err := normalizeHostNetwork(
		options.Network,
		options.PrivateNet,
	)
	if err != nil {
		return nil, err
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = defaultNomadNamespace
	}
	if hostNetwork != "loopback" &&
		namespace == defaultNomadNamespace {
		return nil, fmt.Errorf(
			"production host network %q refuses the shared Nomad namespace %q; "+
				"create a dedicated namespace and pass -namespace",
			hostNetwork,
			defaultNomadNamespace,
		)
	}
	agentURL := strings.TrimSuffix(strings.TrimSpace(options.AgentURL), "/")
	if agentURL == "" && hostNetwork == "loopback" {
		agentURL = fmt.Sprintf("http://127.0.0.1:%d", options.AgentPort)
	}
	if agentURL != "" {
		if _, err := absoluteHTTPURL(agentURL); err != nil {
			return nil, fmt.Errorf("agent URL: %w", err)
		}
	}
	allowedOrigins := ""
	if requirePrerequisites {
		allowedOrigins, err = normalizeAllowedOrigins(
			options.AllowedOrigins,
			hostNetwork,
		)
		if err != nil {
			return nil, err
		}
	}
	gmCharacters, err := configuredGMCharacters(
		options.GMCharacters,
		hostNetwork,
		stateDir,
	)
	if err != nil {
		return nil, fmt.Errorf("GM allowlist: %w", err)
	}
	bugReports, warnings := bugreport.LoadConfig(os.Getenv)
	for _, warning := range warnings {
		fmt.Printf("Bug reports: %s\n", warning)
	}
	identityIssuer := strings.TrimSuffix(
		strings.TrimSpace(options.IdentityIssuer),
		"/",
	)
	identityJWKSURL := strings.TrimSpace(options.IdentityJWKSURL)
	if requirePrerequisites {
		if identityIssuer == "" && hostNetwork == "loopback" {
			identityIssuer = developmentNomadAddress
		}
		if identityIssuer == "" {
			return nil, fmt.Errorf(
				"-identity-issuer is required outside loopback",
			)
		}
		if _, err := secureIdentityURL(identityIssuer); err != nil {
			return nil, fmt.Errorf("nomad identity issuer: %w", err)
		}
		if identityJWKSURL == "" {
			identityJWKSURL = identityIssuer + "/.well-known/jwks.json"
		}
		if _, err := secureIdentityURL(identityJWKSURL); err != nil {
			return nil, fmt.Errorf("nomad identity JWKS URL: %w", err)
		}
	}

	shards := make([]shardDeployment, 0)
	for _, definition := range catalog.Definitions() {
		if !definition.Enabled {
			continue
		}
		controlPort, err := endpointPort(definition.ControlURL)
		if err != nil {
			return nil, fmt.Errorf(
				"shard %q control URL: %w",
				definition.ID,
				err,
			)
		}
		transportPort, err := endpointPort(definition.TransportURL)
		if err != nil {
			return nil, fmt.Errorf(
				"shard %q transport URL: %w",
				definition.ID,
				err,
			)
		}
		authorityDir := filepath.Join(
			moduleRoot,
			".state",
			"shards",
			definition.ID,
			"authority",
		)
		if requirePrerequisites {
			err = requireRegularFile(
				filepath.Join(authorityDir, store.DBFileName),
			)
		}
		if err != nil {
			return nil, fmt.Errorf(
				"shard %q authority: %w",
				definition.ID,
				err,
			)
		}
		shards = append(shards, shardDeployment{
			Definition:    definition,
			ControlPort:   controlPort,
			TransportPort: transportPort,
			AuthorityDir:  cleanAbsolute(authorityDir),
		})
	}
	if requirePrerequisites && len(shards) == 0 {
		return nil, fmt.Errorf("shard catalog has no enabled shards")
	}
	transportCert, transportKey, transportTLSID, err :=
		resolveTransportCertificate(
			options.TransportCert,
			options.TransportKey,
			hostNetwork,
			requirePrerequisites,
			shards,
		)
	if err != nil {
		return nil, err
	}

	return &deployment{
		ModuleRoot:      moduleRoot,
		Catalog:         cleanAbsolute(catalogPath),
		StateDir:        stateDir,
		ReleaseDir:      releaseDir,
		JobsDir:         cleanAbsolute(jobsDir),
		Namespace:       namespace,
		Network:         hostNetwork,
		AllowedOrigins:  allowedOrigins,
		GMCharacters:    gmCharacters,
		TransportCert:   transportCert,
		TransportKey:    transportKey,
		TransportTLSID:  transportTLSID,
		IdentityIssuer:  identityIssuer,
		IdentityJWKSURL: identityJWKSURL,
		AgentURL:        agentURL,
		AgentPort:       options.AgentPort,
		PrivateNetwork:  options.PrivateNet,
		TaskUser:        options.TaskUser,
		TaskUID:         taskUID,
		TaskGID:         taskGID,
		AgentCPU:        options.AgentCPU,
		AgentMemoryMB:   options.AgentMemoryMB,
		GameCPU:         options.GameCPU,
		GameMemoryMB:    options.GameMemoryMB,
		AgentReleaseID:  agentReleaseID,
		GameReleaseID:   gameReleaseID,
		AgentSource:     filepath.Join(moduleRoot, binaryName("agent")),
		GameSource:      filepath.Join(moduleRoot, binaryName("gameworld")),
		AgentBinary: filepath.Join(
			releaseDir,
			"agent",
			agentReleaseID,
			binaryName("agent"),
		),
		GameBinary: filepath.Join(
			releaseDir,
			"gameworld",
			gameReleaseID,
			binaryName("gameworld"),
		),
		DataPaths:  dataPaths,
		Shards:     shards,
		Secrets:    secrets,
		BugReports: bugReports,
	}, nil
}

/*
================
loadClusterSecrets
================
*/
func loadClusterSecrets(stateDir string) (clusterSecrets, error) {
	accountsPath := filepath.Join(stateDir, "accounts.json")
	if _, err := auth.Load(accountsPath); err != nil {
		return clusterSecrets{}, fmt.Errorf(
			"account catalog %s: %w",
			accountsPath,
			err,
		)
	}
	accountsJSON, err := os.ReadFile(accountsPath)
	if err != nil {
		return clusterSecrets{}, err
	}
	accountChunks, err := chunkAccountCatalog(accountsJSON)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf(
			"account catalog %s: %w",
			accountsPath,
			err,
		)
	}
	keyRingPath := filepath.Join(
		stateDir,
		auth.AgentSessionPrivateKeyRingFile,
	)
	sessionPrivate, err := os.ReadFile(keyRingPath)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w", keyRingPath, err)
	}
	sessionPublic, err := auth.PublicAgentSessionKeyRing(sessionPrivate)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w", keyRingPath, err)
	}
	// The website holds a copy of this token; sro-provision-identity creates it.
	tokenPath := filepath.Join(stateDir, auth.AgentProvisioningTokenFile)
	tokenPayload, err := os.ReadFile(tokenPath)
	if err != nil {
		return clusterSecrets{}, fmt.Errorf("%s: %w (run sro-provision-identity)", tokenPath, err)
	}
	provisioningToken := strings.TrimSpace(string(tokenPayload))
	if len(provisioningToken) < auth.MinProvisioningTokenBytes {
		return clusterSecrets{}, fmt.Errorf(
			"%s: token is shorter than %d bytes",
			tokenPath,
			auth.MinProvisioningTokenBytes,
		)
	}
	return clusterSecrets{
		AccountChunks:     accountChunks,
		SessionPrivate:    string(sessionPrivate),
		SessionPublic:     string(sessionPublic),
		ProvisioningToken: provisioningToken,
	}, nil
}

/*
================
endpointPort
================
*/
func endpointPort(raw string) (int, error) {
	endpoint, err := url.Parse(raw)
	if err != nil {
		return 0, err
	}
	_, portText, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		return 0, fmt.Errorf(
			"%q must include an explicit port: %w",
			raw,
			err,
		)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%q has invalid port", raw)
	}
	return port, nil
}

/*
================
absoluteHTTPURL
================
*/
func absoluteHTTPURL(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Host == "" ||
		endpoint.User != nil {
		return nil, fmt.Errorf("%q must be an absolute http(s) URL", raw)
	}
	return endpoint, nil
}

/*
================
secureIdentityURL
================
*/
func secureIdentityURL(raw string) (*url.URL, error) {
	parsed, err := absoluteHTTPURL(raw)
	if err != nil {
		return nil, err
	}
	host := parsed.Hostname()
	loopback := strings.EqualFold(host, "localhost")
	if !loopback {
		if ip := net.ParseIP(host); ip != nil {
			loopback = ip.IsLoopback()
		}
	}
	if parsed.Scheme != "https" &&
		(parsed.Scheme != "http" || !loopback) {
		return nil, fmt.Errorf("must use HTTPS outside loopback")
	}
	return parsed, nil
}

/*
================
buildBinaries
================
*/
func (deployment *deployment) buildBinaries(ctx context.Context) error {
	commands := [][]string{
		{"build", "-o", binaryName("agent"), "./cmd/services/sro-agent"},
		{"build", "-o", binaryName("gameworld"), "./cmd/services/sro-gameworld"},
	}
	for _, arguments := range commands {
		command := exec.CommandContext(ctx, "go", arguments...)
		command.Dir = deployment.ModuleRoot
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf(
				"go %s: %w",
				strings.Join(arguments, " "),
				err,
			)
		}
	}
	return nil
}

/*
================
jobTemplates
================
*/
func (deployment *deployment) jobTemplates() ([]byte, []byte, error) {
	agentTemplate, err := os.ReadFile(
		filepath.Join(deployment.JobsDir, agentTemplateName),
	)
	if err != nil {
		return nil, nil, err
	}
	gameTemplate, err := os.ReadFile(
		filepath.Join(deployment.JobsDir, gameTemplateName),
	)
	if err != nil {
		return nil, nil, err
	}
	return agentTemplate, gameTemplate, nil
}

/*
================
agentVariables
================
*/
func (deployment *deployment) agentVariables() map[string]any {
	return deployment.nodeVariables(deployment.AgentCPU, deployment.AgentMemoryMB, map[string]any{
		"binary_path":  slashPath(deployment.AgentBinary),
		"catalog_path": slashPath(deployment.Catalog),
		"directory_state_path": slashPath(filepath.Join(
			deployment.StateDir,
			"agent",
			"shard-leases.json",
		)),
		"accounts_db_path": slashPath(filepath.Join(
			deployment.StateDir,
			"agent",
			"accounts.db",
		)),
		"host_network":      deployment.Network,
		"nomad_namespace":   deployment.Namespace,
		"agent_port":        deployment.AgentPort,
		"release_id":        deployment.AgentReleaseID,
		"private_network":   boolEnvValue(deployment.PrivateNetwork),
		"allowed_origins":   deployment.AllowedOrigins,
		"identity_issuer":   deployment.IdentityIssuer,
		"identity_jwks_url": deployment.IdentityJWKSURL,
		"bug_report_replay_default": boolEnvValue(
			deployment.BugReports.ReplayDefault,
		),
		"bug_report_max_bytes": strconv.FormatInt(
			deployment.BugReports.MaxBytes,
			10,
		),
	})
}

/*
================
gameVariables
================
*/
func (deployment *deployment) gameVariables(
	game shardDeployment,
) map[string]any {
	return deployment.nodeVariables(deployment.GameCPU, deployment.GameMemoryMB, map[string]any{
		"shard_id":                         game.Definition.ID,
		"binary_path":                      slashPath(deployment.GameBinary),
		"catalog_path":                     slashPath(deployment.Catalog),
		"authority_dir":                    slashPath(game.AuthorityDir),
		"server_game_data_root":            slashPath(deployment.DataPaths.RuntimeRoot),
		"server_game_data_manifest_digest": deployment.DataPaths.ManifestDigest,
		"cert_dir": slashPath(filepath.Join(
			deployment.StateDir,
			"dev-certs",
		)),
		"transport_cert_file": deployment.TransportCert,
		"transport_key_file":  deployment.TransportKey,
		"transport_tls_id":    deployment.TransportTLSID,
		"host_network":        deployment.Network,
		"nomad_namespace":     deployment.Namespace,
		"control_port":        game.ControlPort,
		"transport_port":      game.TransportPort,
		"release_id":          deployment.GameReleaseID,
		"private_network":     boolEnvValue(deployment.PrivateNetwork),
		"allowed_origins":     deployment.AllowedOrigins,
		"gm_characters":       deployment.GMCharacters,
	})
}

/*
================
renderGameWorldTemplate
================
*/
func renderGameWorldTemplate(template []byte, shardID string) []byte {
	return bytes.ReplaceAll(
		template,
		[]byte(gameWorldShardToken),
		[]byte(shardID),
	)
}

/*
================
renderAgentTemplate
================
*/
func renderAgentTemplate(
	template []byte,
	chunkCount int,
) ([]byte, error) {
	if bytes.Count(template, []byte(agentAccountToken)) != 1 {
		return nil, fmt.Errorf(
			"agent job template must contain exactly one %s token",
			agentAccountToken,
		)
	}
	if chunkCount < 1 {
		return nil, fmt.Errorf("agent account catalog has no chunks")
	}
	if chunkCount > maxAccountChunks {
		return nil, fmt.Errorf(
			"agent account catalog requires %d chunks, limit is %d",
			chunkCount,
			maxAccountChunks,
		)
	}
	var rendered strings.Builder
	for index := 0; index < chunkCount; index++ {
		fmt.Fprintf(
			&rendered,
			`{{ with nomadVar %q }}{{ .payload }}{{ end }}`,
			accountChunkVariablePath(index),
		)
		rendered.WriteByte('\n')
	}
	return bytes.Replace(
		template,
		[]byte(agentAccountToken),
		[]byte(rendered.String()),
		1,
	), nil
}

/*
================
chunkAccountCatalog
================
*/
func chunkAccountCatalog(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("file is not valid UTF-8")
	}
	chunks := make([]string, 0, (len(payload)/accountChunkBytes)+1)
	for start := 0; start < len(payload); {
		end := start + accountChunkBytes
		if end >= len(payload) {
			end = len(payload)
		} else {
			for end > start && !utf8.RuneStart(payload[end]) {
				end--
			}
		}
		if end == start {
			return nil, fmt.Errorf(
				"cannot split UTF-8 account catalog at byte %d",
				start,
			)
		}
		chunks = append(chunks, string(payload[start:end]))
		start = end
	}
	return chunks, nil
}

/*
================
boolEnvValue
================
*/
func boolEnvValue(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

/*
================
normalizeHostNetwork
================
*/
func normalizeHostNetwork(name string, private bool) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("host network name is required")
	}
	if strings.EqualFold(name, "loopback") {
		return "loopback", nil
	}
	if !private {
		return "", fmt.Errorf(
			"host network %q requires -private-network acknowledgment",
			name,
		)
	}
	return name, nil
}

/*
================
configuredGMCharacters

Host-owned durable configuration survives deploys launched by another shell.
An existing empty file deliberately revokes all grants, including dev defaults.
================
*/
func configuredGMCharacters(raw, hostNetwork, stateDir string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		data, err := os.ReadFile(filepath.Join(stateDir, "gm-characters.txt"))
		if err == nil {
			value := strings.TrimSpace(string(data))
			if _, err := store.ParseGMCharacters(value); err != nil {
				return "", err
			}
			return value, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
	return normalizeGMCharacters(raw, hostNetwork)
}

/*
================
normalizeGMCharacters
================
*/
func normalizeGMCharacters(raw string, hostNetwork string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && hostNetwork == "loopback" {
		raw = developmentGMCharacters
	}
	if _, err := store.ParseGMCharacters(raw); err != nil {
		return "", err
	}
	return raw, nil
}

/*
================
normalizeAllowedOrigins
================
*/
func normalizeAllowedOrigins(raw string, hostNetwork string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if hostNetwork == "loopback" {
			return developmentAllowedOrigins, nil
		}
		return "", fmt.Errorf(
			"-allowed-origins is required for host network %q",
			hostNetwork,
		)
	}

	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, candidate := range strings.Split(raw, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return "", fmt.Errorf(
				"-allowed-origins contains an empty origin",
			)
		}
		origin, err := url.Parse(candidate)
		if err != nil {
			return "", fmt.Errorf(
				"allowed origin %q: %w",
				candidate,
				err,
			)
		}
		scheme := strings.ToLower(origin.Scheme)
		if scheme != "http" && scheme != "https" {
			return "", fmt.Errorf(
				"allowed origin %q must use http or https",
				candidate,
			)
		}
		if origin.Host == "" ||
			origin.Hostname() == "" ||
			origin.User != nil ||
			(origin.Path != "" && origin.Path != "/") ||
			origin.RawQuery != "" ||
			origin.Fragment != "" {
			return "", fmt.Errorf(
				"allowed origin %q must be an origin without "+
					"credentials, path, query, or fragment",
				candidate,
			)
		}
		if portText := origin.Port(); portText != "" {
			port, err := strconv.Atoi(portText)
			if err != nil || port < 1 || port > 65535 {
				return "", fmt.Errorf(
					"allowed origin %q has an invalid port",
					candidate,
				)
			}
		}
		if strings.Contains(origin.Host, "*") {
			return "", fmt.Errorf(
				"allowed origin %q cannot use a wildcard host",
				candidate,
			)
		}
		canonical := scheme +
			"://" + strings.ToLower(origin.Host)
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		origins = append(origins, canonical)
	}
	sort.Strings(origins)
	return strings.Join(origins, ","), nil
}

/*
================
requireRegularFile
================
*/
func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("required file %s is not regular", path)
	}
	return nil
}

/*
================
cleanAbsolute
================
*/
func cleanAbsolute(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

/*
================
slashPath
================
*/
func slashPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}
