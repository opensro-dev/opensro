/*
===========================================================================

accounts.go - the Agent's account authority and provisioning listener

The Agent opens its live account database (auth.Accounts), seeds it from the
deploy-time catalog when one is supplied, and, when a provisioning token is
configured, serves the private provisioning API (internal/agent/provisioning)
on a second, loopback-only listener. That listener never shares the port the
edge proxies, so no edge route can reach it.

===========================================================================
*/
package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/agent/provisioning"
	"opensro.online/server/internal/security/auth"
)

const (
	envAccountsDBPath         = "SRO_AGENT_ACCOUNTS_DB_PATH"
	envProvisioningAddr       = "SRO_AGENT_PROVISIONING_ADDR"
	envProvisioningTokenPath  = "SRO_AGENT_PROVISIONING_TOKEN_PATH"
	defaultAccountsDBPath     = ".state/agent/accounts.db"
	defaultProvisioningAddr   = "127.0.0.1:8789"
	maxProvisioningTokenBytes = 4 << 10
)

/*
================
openAccountAuthority

Opens the account database and inserts any seed-catalog account it lacks.
An authority with no accounts is refused: GameWorld treats an empty account
directory as a failure, and nobody could log in.
================
*/
func openAccountAuthority() (*auth.Accounts, error) {
	path := strings.TrimSpace(os.Getenv(envAccountsDBPath))
	if path == "" {
		path = defaultAccountsDBPath
	}
	accounts, err := auth.OpenAccounts(path)
	if err != nil {
		return nil, fmt.Errorf("account database %s: %w", path, err)
	}
	if seedPath := strings.TrimSpace(os.Getenv(envAccountsPath)); seedPath != "" {
		catalog, err := auth.Load(seedPath)
		if err != nil {
			_ = accounts.Close()
			return nil, fmt.Errorf("seed catalog %s: %w", seedPath, err)
		}
		inserted, err := accounts.Seed(catalog)
		if err != nil {
			_ = accounts.Close()
			return nil, fmt.Errorf("seed catalog %s: %w", seedPath, err)
		}
		if inserted > 0 {
			log.Infof("agent: seeded %d account(s) from %s", inserted, seedPath)
		}
	}
	if accounts.Len() == 0 {
		_ = accounts.Close()
		return nil, fmt.Errorf("account database %s holds no accounts and no seed catalog added any", path)
	}
	return accounts, nil
}

/*
================
readProvisioningToken

The token file, or "" when provisioning is not configured (no path, or an
empty file: the deploy renders an empty file when no token exists).
================
*/
func readProvisioningToken() (string, error) {
	path := strings.TrimSpace(os.Getenv(envProvisioningTokenPath))
	if path == "" {
		return "", nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxProvisioningTokenBytes {
		return "", fmt.Errorf("%s must be a regular file of at most %d bytes", path, maxProvisioningTokenBytes)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(payload)), nil
}

/*
================
startProvisioning

Returns a nil server when provisioning is not configured, else the server
and the address it bound. The address must be
loopback; unlike the Agent API there is no private-network override.
================
*/
func startProvisioning(accounts *auth.Accounts, serveErrors chan<- error) (*http.Server, string, error) {
	token, err := readProvisioningToken()
	if err != nil {
		return nil, "", fmt.Errorf("provisioning token: %w", err)
	}
	if token == "" {
		log.Info("agent: account provisioning API disabled (no token)")
		return nil, "", nil
	}
	api, err := provisioning.New(accounts, token)
	if err != nil {
		return nil, "", err
	}
	addr := strings.TrimSpace(os.Getenv(envProvisioningAddr))
	if addr == "" {
		addr = defaultProvisioningAddr
	}
	if !loopbackAddress(addr) {
		return nil, "", fmt.Errorf("%s=%s must be a loopback address", envProvisioningAddr, addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("listen %s: %w", addr, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("provisioning listener: %w", err)
		}
	}()
	log.Infof("agent: account provisioning API on http://%s", listener.Addr())
	return server, listener.Addr().String(), nil
}
