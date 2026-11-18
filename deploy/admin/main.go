/*
===========================================================================

main.go - the admin panel: staff pages over the shard's private APIs

Two modes, one binary:

	admin serve
	    The panel's pages. Runs in the GameWorld's network namespace
	    (docker compose network_mode: service:gameworld-<shard>), because
	    the player operations answer only a loopback peer on the
	    GameWorld's control port.

	admin relay
	    Runs in the Agent's namespace and forwards the account routes the
	    panel uses to the Agent's loopback-only provisioning API. The
	    provisioning token still rides every request; the relay adds no
	    credential of its own.

Neither mode publishes anything to the internet: the compose file binds
the panel to the host's loopback and staff reach it through an SSH
tunnel (deploy/docs/2026-11-17-admin-panel-design.md).

===========================================================================
*/
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// Defaults match deploy/docker-compose.prod.yml.
const (
	defaultPanelListen       = "0.0.0.0:9090"
	defaultOperatorURL       = "http://127.0.0.1:8791"
	defaultAccountsURL       = "http://agent:9789"
	defaultOperatorToken     = "/admin/operator-token"
	defaultProvisioningToken = "/admin/agent-provisioning-token"
	defaultOperatorName      = "admin"
	defaultRelayListen       = "0.0.0.0:9789"
	defaultRelayTarget       = "http://127.0.0.1:8789"
)

// upstreamTimeout bounds one call to a private API; a player operation
// waits up to 5 s for the character's session to close.
const upstreamTimeout = 15 * time.Second

/*
================
main
================
*/
func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	var handler http.Handler
	var listen string
	switch mode {
	case "serve":
		panel, err := newPanel(panelConfig{
			OperatorURL:           env("ADMIN_OPERATOR_URL", defaultOperatorURL),
			OperatorTokenFile:     env("ADMIN_OPERATOR_TOKEN_FILE", defaultOperatorToken),
			AccountsURL:           env("ADMIN_ACCOUNTS_URL", defaultAccountsURL),
			ProvisioningTokenFile: env("ADMIN_PROVISIONING_TOKEN_FILE", defaultProvisioningToken),
			OperatorName:          env("ADMIN_OPERATOR_NAME", defaultOperatorName),
		})
		if err != nil {
			log.Fatal(err)
		}
		handler, listen = panel.Handler(), env("ADMIN_LISTEN", defaultPanelListen)
	case "relay":
		relay, err := newRelay(env("RELAY_TARGET", defaultRelayTarget))
		if err != nil {
			log.Fatal(err)
		}
		handler, listen = relay, env("RELAY_LISTEN", defaultRelayListen)
	default:
		fmt.Fprintln(os.Stderr, "usage: admin serve | admin relay")
		os.Exit(2)
	}
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("admin %s listening on %s", mode, listen)
	log.Fatal(server.ListenAndServe())
}

/*
================
env
================
*/
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
