// Command sro-agent runs the global Agent/Login control-plane process.
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/server"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/config"
	"opensro.online/server/internal/platform/logging"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/security/workload"
)

const (
	// envAccountsPath names the optional seed catalog (accounts.go).
	envAccountsPath       = "SRO_AGENT_ACCOUNTS_PATH"
	envSessionKeyRingPath = "SRO_AGENT_SESSION_KEYRING_PATH"
	envNomadIssuer        = "SRO_NOMAD_IDENTITY_ISSUER"
	envNomadJWKS          = "SRO_NOMAD_JWKS_URL"
	envNomadNamespace     = "SRO_NOMAD_NAMESPACE"
	envAgentAddr          = "SRO_AGENT_API_ADDR"
	envPrivateNetwork     = "SRO_AGENT_PRIVATE_NETWORK"
	envOrigins            = "SRO_AGENT_ALLOWED_ORIGINS"
	envDirectoryPath      = "SRO_AGENT_DIRECTORY_STATE_PATH"
	defaultDirectoryPath  = ".state/agent/shard-leases.json"
)

func main() {
	logging.InstallFormat()
	config.Initialize()
	logging.Init()

	root, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	catalog, catalogPath, err := shard.LoadFromEnv()
	if err != nil {
		log.Fatalf("agent: shard catalog %s: %v", catalogPath, err)
	}
	directoryPath := strings.TrimSpace(os.Getenv(envDirectoryPath))
	if directoryPath == "" {
		directoryPath = defaultDirectoryPath
	}
	directory, err := shard.NewPersistentDirectory(
		catalog,
		shard.DefaultLeaseTTL,
		directoryPath,
	)
	if err != nil {
		log.Fatalf("agent: shard directory: %v", err)
	}
	accounts, err := openAccountAuthority()
	if err != nil {
		log.Fatalf("agent: accounts: %v", err)
	}
	defer accounts.Close()
	sessionSigner, err := auth.NewAgentSessionSigner(
		strings.TrimSpace(os.Getenv(envSessionKeyRingPath)),
	)
	if err != nil {
		log.Fatalf("agent: session signing keys: %v", err)
	}
	controlVerifier, err := workload.NewNomadWorkloadIdentityVerifier(
		os.Getenv(envNomadIssuer),
		os.Getenv(envNomadJWKS),
		nil,
	)
	if err != nil {
		log.Fatalf("agent: Nomad workload identity: %v", err)
	}
	nomadNamespace := strings.TrimSpace(os.Getenv(envNomadNamespace))
	if nomadNamespace == "" {
		log.Fatalf("agent: %s is required", envNomadNamespace)
	}
	ready := readiness.NewGate()
	server, err := agentserver.New(agentserver.Config{
		Accounts:                accounts,
		Catalog:                 catalog,
		Directory:               directory,
		SessionSigner:           sessionSigner,
		ControlIdentityVerifier: controlVerifier,
		ControlNamespace:        nomadNamespace,
		AllowedOrigins:          splitCSV(os.Getenv(envOrigins)),
		Readiness:               ready,
	})
	if err != nil {
		log.Fatalf("agent: construction: %v", err)
	}

	addr := strings.TrimSpace(os.Getenv(envAgentAddr))
	if addr == "" {
		addr = agentapi.DefaultAddr
	}
	privateNetwork := os.Getenv(envPrivateNetwork) == "1"
	if !agentListenAllowed(addr, privateNetwork) {
		log.Fatalf(
			"agent: %s must be loopback unless %s=1 acknowledges a private network behind a TLS edge",
			envAgentAddr,
			envPrivateNetwork,
		)
	}
	if !loopbackAddress(addr) {
		log.Warnf(
			"agent: private-network listener %s; never publish this plaintext port directly",
			addr,
		)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("agent: listen %s: %v", addr, err)
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	// One slot per listener, so neither goroutine blocks on a failure.
	serveErrors := make(chan error, 2)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()
	provisioningServer, _, err := startProvisioning(accounts, serveErrors)
	if err != nil {
		log.Fatalf("agent: account provisioning: %v", err)
	}
	ready.Open()
	log.Infof(
		"agent: serving %d account(s) and %d shard definition(s) on http://%s",
		accounts.Len(),
		len(catalog.Definitions()),
		listener.Addr(),
	)

	var serveError error
	select {
	case <-root.Done():
		log.Info("agent: termination signal received")
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("agent: listener failed: %v", err)
			serveError = err
		}
	}
	ready.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := httpServer.Shutdown(shutdown); err != nil {
		log.Errorf("agent: shutdown: %v", err)
		if serveError == nil {
			serveError = err
		}
	}
	if provisioningServer != nil {
		if err := provisioningServer.Shutdown(shutdown); err != nil {
			log.Errorf("agent: provisioning shutdown: %v", err)
		}
	}
	cancel()
	if serveError != nil {
		// os.Exit skips deferred calls; close the account database first.
		_ = accounts.Close()
		os.Exit(1)
	}
}

func agentListenAllowed(address string, privateNetwork bool) bool {
	return loopbackAddress(address) || privateNetwork
}

func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return agentapi.DefaultAllowedOrigins()
	}
	var values []string
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}
