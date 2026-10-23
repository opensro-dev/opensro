//go:build !opensro_ops

/*
===========================================================================

operator_commands_off.go - a developer build has no operator subcommands

See operator_commands.go: they are built only with -tags opensro_ops.

===========================================================================
*/
package main

// operatorCommands is empty without the operator build.
var operatorCommands = map[string]operatorCommand{}
