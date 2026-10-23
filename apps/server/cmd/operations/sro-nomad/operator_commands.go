//go:build opensro_ops

/*
===========================================================================

operator_commands.go - the production operator's subcommands

A running shard's operator publishes browser releases, posts maintenance
notices, rotates the Agent's session key and lists the maintenance probe
accounts. A developer's local server needs none of them, so they are built
only with -tags opensro_ops (the operations repository's builds).

===========================================================================
*/
package main

import "context"

// operatorCommands are dispatched by main ahead of the fleet commands.
var operatorCommands = map[string]operatorCommand{
	"publish-client":     {run: func(_ context.Context, arguments []string) error { return runPublishClient(arguments) }},
	"notice":             {run: runNotice, signals: true},
	"rotate-session-key": {run: runRotateSessionKey, signals: true},
	"maintenance-list": {run: func(_ context.Context, arguments []string) error { return runMaintenanceList(arguments) },
		signals: true},
}
