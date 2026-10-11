/*
===========================================================================

variablestops_test.go - what a changed credential variable needs

A variable that differs while its job is registered refuses a deployment;
the staging rehearsal (validate -assume-stopped) plans as if the fleet were
stopped and reports each such variable instead, so a release that needs a
stop is known before its window (the 2026-10-11 F2 publish failed here).

===========================================================================
*/
package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

/*
================
registeredJobs

A job registry fake: the named jobs are registered.
================
*/
func registeredJobs(names ...string) func(string) (bool, error) {
	return func(jobID string) (bool, error) {
		for _, name := range names {
			if name == jobID {
				return true, nil
			}
		}
		return false, nil
	}
}

/*
================
TestVariableStopsRefusesWhileRegistered
================
*/
func TestVariableStopsRefusesWhileRegistered(t *testing.T) {
	changed := map[string]string{"sro-gameworld-1": "nomad/jobs/sro-gameworld-1/gameworld/gameworld"}
	_, err := variableStops(changed, registeredJobs("sro-gameworld-1"), false)
	if err == nil || !strings.Contains(err.Error(), "differs while job sro-gameworld-1 is registered") {
		t.Fatalf("refusal = %v", err)
	}
}

/*
================
TestVariableStopsAssumeStoppedReportsInsteadOfRefusing
================
*/
func TestVariableStopsAssumeStoppedReportsInsteadOfRefusing(t *testing.T) {
	changed := map[string]string{
		"sro-gameworld-1": "nomad/jobs/sro-gameworld-1/gameworld/gameworld",
		"sro-agent":       "nomad/jobs/sro-agent/agent/agent",
		"sro-gameworld-2": "nomad/jobs/sro-gameworld-2/gameworld/gameworld",
	}
	stops, err := variableStops(changed, registeredJobs("sro-gameworld-1", "sro-agent"), true)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by job; an unregistered job's variable is simply written.
	want := []stopRequirement{
		{jobID: "sro-agent", path: "nomad/jobs/sro-agent/agent/agent"},
		{jobID: "sro-gameworld-1", path: "nomad/jobs/sro-gameworld-1/gameworld/gameworld"},
	}
	if !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %+v, want %+v", stops, want)
	}
}

/*
================
TestVariableStopsNothingChanged
================
*/
func TestVariableStopsNothingChanged(t *testing.T) {
	for _, assumeStopped := range []bool{false, true} {
		stops, err := variableStops(map[string]string{}, registeredJobs("sro-agent"), assumeStopped)
		if err != nil || len(stops) != 0 {
			t.Fatalf("assumeStopped %v: stops %v, err %v", assumeStopped, stops, err)
		}
	}
}

/*
================
TestVariableStopsRegistryErrorPropagates
================
*/
func TestVariableStopsRegistryErrorPropagates(t *testing.T) {
	failed := errors.New("nomad unreachable")
	_, err := variableStops(map[string]string{"sro-agent": "p"}, func(string) (bool, error) {
		return false, failed
	}, true)
	if !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
}

/*
================
TestAssumeStoppedOnlyForValidate
================
*/
func TestAssumeStoppedOnlyForValidate(t *testing.T) {
	_, err := parseOptions("deploy", []string{"-assume-stopped"})
	if err == nil || !strings.Contains(err.Error(), "only by validate") {
		t.Fatalf("deploy -assume-stopped = %v", err)
	}
}
