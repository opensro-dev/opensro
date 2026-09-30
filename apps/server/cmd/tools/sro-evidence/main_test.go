package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestEvidenceCommandCatalog(t *testing.T) {
	want := []string{
		"moveclip-oracle",
		"navsweep",
		"performance-ring",
		"spawnable-npcs",
		"spawnable-monsters",
	}
	got := make([]string, 0, len(evidenceCommands))
	seen := make(map[string]bool, len(evidenceCommands))
	for _, command := range evidenceCommands {
		if command.name == "" || command.summary == "" || command.run == nil {
			t.Fatalf("incomplete command registration: %+v", command)
		}
		if seen[command.name] {
			t.Fatalf("duplicate command %q", command.name)
		}
		seen[command.name] = true
		got = append(got, command.name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestSelectEvidenceCommand(t *testing.T) {
	command, tail, err := selectEvidenceCommand([]string{"moveclip-oracle", "-chords", "input.json"})
	if err != nil {
		t.Fatal(err)
	}
	if command.name != "moveclip-oracle" || !reflect.DeepEqual(tail, []string{"-chords", "input.json"}) {
		t.Fatalf("selection = %q %v", command.name, tail)
	}

	for _, args := range [][]string{nil, {"unknown"}} {
		if _, _, err := selectEvidenceCommand(args); !errors.Is(err, errCommandUsage) {
			t.Fatalf("select %v error = %v, want command usage", args, err)
		}
	}
}
