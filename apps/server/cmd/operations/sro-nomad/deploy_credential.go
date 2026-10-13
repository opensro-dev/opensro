package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/config"
)

const (
	productionDeployPolicy      = "sro-deployer"
	minimumDeployTokenRemaining = 20 * time.Minute
	maximumDeployTokenLifetime  = 2 * time.Hour
	nomadClientTokenType        = "client"
)

func requireDeploymentCredential(
	ctx context.Context,
	client *nomadClient,
	options commandOptions,
) error {
	// The checkout-owned development agent intentionally runs without ACLs.
	// "default" is only a namespace name, not proof of that topology: a remote
	// or forwarded production cluster may also expose it. Exempt credentials
	// only after the listener proves the same exact identity as dev-agent.
	if options.Namespace == defaultNomadNamespace {
		if err := requireCheckoutDevelopmentAgent(
			ctx,
			client,
			options,
		); err != nil {
			return fmt.Errorf(
				"unsafe credential exemption for Nomad namespace %q: %w; use -namespace sro and a short-lived %s token for every other cluster",
				defaultNomadNamespace,
				err,
				productionDeployPolicy,
			)
		}
		return nil
	}
	token, _, err := client.api.ACLTokens().Self(
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("inspect Nomad deployment credential: %w", err)
	}
	if err := validateProductionDeployCredential(token, time.Now()); err != nil {
		return fmt.Errorf("unsafe Nomad deployment credential: %w", err)
	}
	return nil
}

func requireCheckoutDevelopmentAgent(
	ctx context.Context,
	client *nomadClient,
	options commandOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	address := strings.TrimRight(client.api.Address(), "/")

	moduleRoot := strings.TrimSpace(options.ModuleRoot)
	if moduleRoot == "" {
		var err error
		moduleRoot, err = config.FindModuleRoot()
		if err != nil {
			return err
		}
	}
	moduleRoot = cleanAbsolute(moduleRoot)
	expectation := devAgentExpectation{
		configPath: filepath.Join(
			moduleRoot,
			"ops",
			"nomad",
			"config",
			"dev-windows.hcl",
		),
		dataDir: filepath.Join(
			moduleRoot,
			".state",
			"nomad",
			"dev-agent",
		),
		oidcIssuer: options.DevStack.nomadAddress(),
	}

	self, err := client.api.Agent().Self()
	if err != nil {
		return fmt.Errorf("inspect development Nomad agent identity: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCheckoutDevelopmentAgent(
		address,
		self,
		expectation,
	); err != nil {
		return fmt.Errorf(
			"listener is not the development agent for this checkout: %w",
			err,
		)
	}
	return nil
}

func validateCheckoutDevelopmentAgent(
	address string,
	self *nomad.AgentSelf,
	expectation devAgentExpectation,
) error {
	// The development agent's API address is its identity issuer
	// (dev-windows.hcl; devStack.portsConfig when the stack is moved).
	if strings.TrimRight(address, "/") != expectation.oidcIssuer {
		return fmt.Errorf(
			"nomad API is %q, want checkout development address %q",
			address,
			expectation.oidcIssuer,
		)
	}
	return validateDevAgentIdentity(self, expectation)
}

func validateProductionDeployCredential(
	token *nomad.ACLToken,
	now time.Time,
) error {
	if token == nil {
		return fmt.Errorf("nomad returned no token identity")
	}
	if token.Type != nomadClientTokenType {
		return fmt.Errorf(
			"token type is %q; use a client token, never a management token",
			token.Type,
		)
	}
	if token.Global {
		return fmt.Errorf("token is global; issue a region-local token")
	}
	if len(token.Roles) != 0 {
		return fmt.Errorf(
			"token uses ACL roles; attach only policy %q directly",
			productionDeployPolicy,
		)
	}
	if len(token.Policies) != 1 ||
		token.Policies[0] != productionDeployPolicy {
		return fmt.Errorf(
			"token must have exactly policy %q",
			productionDeployPolicy,
		)
	}
	if token.CreateTime.IsZero() {
		return fmt.Errorf("token has no creation time")
	}
	if token.ExpirationTime == nil {
		return fmt.Errorf(
			"token does not expire; issue it with a bounded TTL",
		)
	}
	if !token.ExpirationTime.After(token.CreateTime) {
		return fmt.Errorf("token expiration does not follow its creation")
	}
	lifetime := token.ExpirationTime.Sub(token.CreateTime)
	if lifetime > maximumDeployTokenLifetime {
		return fmt.Errorf(
			"token lifetime is %s; maximum is %s",
			lifetime.Round(time.Second),
			maximumDeployTokenLifetime,
		)
	}
	remaining := token.ExpirationTime.Sub(now)
	if remaining < minimumDeployTokenRemaining {
		return fmt.Errorf(
			"token has %s remaining; at least %s is required",
			remaining.Round(time.Second),
			minimumDeployTokenRemaining,
		)
	}
	return nil
}
