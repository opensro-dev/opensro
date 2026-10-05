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

The arm64 override declares the host's cores at the nominal
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
		t.Fatalf("platform config = %q, want 10 cores at %d MHz", written, arm64CoreMHz)
	}
	if got := devAgentPlatformConfig(8); !strings.Contains(got, "cpu_total_compute = 24000") {
		t.Fatalf("8-core config = %q", got)
	}
}

/*
================
TestDevAgentNeedsPlatformConfig

Only the arm64 hosts whose CPU fingerprint is unusable get the override;
every other host keeps Nomad's own fingerprint.
================
*/
func TestDevAgentNeedsPlatformConfig(t *testing.T) {
	for _, test := range []struct {
		goos, goarch string
		want         bool
	}{
		{"darwin", "arm64", true},
		{"linux", "arm64", true},
		{"darwin", "amd64", false},
		{"linux", "amd64", false},
		{"windows", "amd64", false},
		{"windows", "arm64", false},
	} {
		if got := devAgentNeedsPlatformConfig(test.goos, test.goarch); got != test.want {
			t.Errorf("%s/%s = %v, want %v", test.goos, test.goarch, got, test.want)
		}
	}
}
