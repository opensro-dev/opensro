package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestDevAgentPlatformConfigScalesWithCores

The Apple Silicon override declares the host's cores at the nominal
per-core compute, so any core count can place the development jobs.
================
*/
func TestDevAgentPlatformConfigScalesWithCores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev-agent-platform.hcl")
	if err := writeDevAgentPlatformConfig(path, 10); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "cpu_total_compute = 30000") {
		t.Fatalf("platform config = %q, want 10 cores at %d MHz", written, appleSiliconCoreMHz)
	}
	if got := devAgentPlatformConfig(8); !strings.Contains(got, "cpu_total_compute = 24000") {
		t.Fatalf("8-core config = %q", got)
	}
}
