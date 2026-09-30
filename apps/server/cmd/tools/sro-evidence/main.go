// Command sro-evidence hosts read-only evidence and asset-roster utilities.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

var errCommandUsage = errors.New("command usage")

type evidenceCommand struct {
	name    string
	summary string
	run     func([]string) error
}

var evidenceCommands = []evidenceCommand{
	{name: "moveclip-oracle", summary: "run shared movement chords through the server clipper", run: runMoveclipOracle},
	{name: "navsweep", summary: "find traps, pockets and one-way edges around a point", run: runNavSweep},
	{name: "performance-ring", summary: "measure worst-case scoped monster population", run: runPerformanceRing},
	{name: "spawnable-npcs", summary: "emit the evidence-filtered NPC asset roster", run: runSpawnableNPCs},
	{name: "spawnable-monsters", summary: "emit the evidence-filtered monster asset roster", run: runSpawnableMonsters},
}

func main() {
	err := execute(os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintf(os.Stderr, "sro-evidence: %v\n", err)
	if errors.Is(err, errCommandUsage) {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	os.Exit(1)
}

func execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: missing subcommand", errCommandUsage)
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(os.Stdout)
		return nil
	}
	command, tail, err := selectEvidenceCommand(args)
	if err != nil {
		return err
	}
	if err := command.run(tail); err != nil {
		return fmt.Errorf("%s: %w", command.name, err)
	}
	return nil
}

func selectEvidenceCommand(args []string) (evidenceCommand, []string, error) {
	if len(args) == 0 {
		return evidenceCommand{}, nil, fmt.Errorf("%w: missing subcommand", errCommandUsage)
	}
	for _, command := range evidenceCommands {
		if args[0] == command.name {
			return command, args[1:], nil
		}
	}
	return evidenceCommand{}, nil, fmt.Errorf("%w: unknown subcommand %q", errCommandUsage, args[0])
}

func printUsage(output *os.File) {
	fmt.Fprintln(output, "usage: sro-evidence <subcommand> [options]")
	fmt.Fprintln(output, "subcommands:")
	for _, command := range evidenceCommands {
		fmt.Fprintf(output, "  %-20s %s\n", command.name, command.summary)
	}
}
