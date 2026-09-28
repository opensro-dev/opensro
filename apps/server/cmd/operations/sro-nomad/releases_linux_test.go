//go:build linux

/*
===========================================================================

releases_linux_test.go - task access under the production receiver umask

Runs the staging owner in a child process so the private umask cannot affect
other tests. Release traversal must work without exposing private ancestors.

===========================================================================
*/
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

/*
================
TestStageReleasesWithPrivateUmask
================
*/
func TestStageReleasesWithPrivateUmask(t *testing.T) {
	const childKey = "SRO_RELEASE_UMASK_TEST_CHILD"
	if os.Getenv(childKey) != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestStageReleasesWithPrivateUmask$", "-test.v")
		command.Env = append(os.Environ(), childKey+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("private-umask child: %v\n%s", err, output)
		}
		return
	}
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	jobs := filepath.Join(root, "jobs")
	if err := os.WriteFile(source, []byte("verified executable fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{agentTemplateName, gameTemplateName} {
		if err := os.WriteFile(filepath.Join(jobs, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	agentID, err := releaseID(source, filepath.Join(jobs, agentTemplateName))
	if err != nil {
		t.Fatal(err)
	}
	gameID, err := releaseID(source, filepath.Join(jobs, gameTemplateName))
	if err != nil {
		t.Fatal(err)
	}
	releases := filepath.Join(root, "releases")
	owner := &deployment{ReleaseDir: releases, JobsDir: jobs, AgentSource: source, GameSource: source,
		AgentReleaseID: agentID, GameReleaseID: gameID,
		AgentBinary: filepath.Join(releases, "agent", agentID, "agent"), GameBinary: filepath.Join(releases, "gameworld", gameID, "gameworld")}
	for attempt := 0; attempt < 2; attempt++ {
		if err := owner.stageReleases(); err != nil {
			t.Fatal(err)
		}
		for _, binary := range []string{owner.AgentBinary, owner.GameBinary} {
			for current := binary; current != root; current = filepath.Dir(current) {
				info, err := os.Stat(current)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != releaseAccessMode {
					t.Fatalf("attempt %d: %s mode %o, want %o", attempt, current, got, releaseAccessMode)
				}
			}
		}
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatal("private ancestor permissions changed")
		}
		// An existing immutable release must also recover from the old publisher.
		for _, binary := range []string{owner.AgentBinary, owner.GameBinary} {
			if err := os.Chmod(filepath.Dir(binary), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
}
