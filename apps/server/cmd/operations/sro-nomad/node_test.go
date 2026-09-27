/*
===========================================================================

node_test.go - host identity and executable access regression tests.

These checks cover the permissions needed when the deployer and the task
run under different Unix accounts, while preserving Windows defaults.

===========================================================================
*/
package main

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

/*
================
TestTaskOwnerPreservesDefaultServiceIdentity
================
*/
func TestTaskOwnerPreservesDefaultServiceIdentity(t *testing.T) {
	uid, gid, err := resolveTaskOwner("")
	if err != nil || uid != -1 || gid != -1 {
		t.Fatalf("default identity = %d:%d, %v", uid, gid, err)
	}
}

/*
================
TestTaskOwnerResolvesUnixAccount
================
*/
func TestTaskOwnerResolvesUnixAccount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix ownership is not used on Windows")
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, err := resolveTaskOwner(account.Username)
	if err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(uid) != account.Uid || strconv.Itoa(gid) != account.Gid {
		t.Fatalf("resolved %d:%d; expected %s:%s", uid, gid, account.Uid, account.Gid)
	}
	if _, _, err := resolveTaskOwner("opensro-no-such-task-account"); err == nil {
		t.Fatal("accepted a missing task account")
	}
}

/*
================
TestStagedReleaseAllowsSeparateTaskAccount
================
*/
func TestStagedReleaseAllowsSeparateTaskAccount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not used on Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "releases", "agent", "identity", "agent")
	if err := os.WriteFile(source, []byte("executable fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageRelease(source, destination); err != nil {
		t.Fatal(err)
	}
	for path := destination; path != root; path = filepath.Dir(path) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		const otherReadExecute = 0o005
		const groupOtherWrite = 0o022
		mode := info.Mode().Perm()
		if mode&otherReadExecute != otherReadExecute || mode&groupOtherWrite != 0 {
			t.Fatalf("task access to %s: mode %o", path, mode)
		}
	}
}
