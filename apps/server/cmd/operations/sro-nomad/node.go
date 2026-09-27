/*
===========================================================================

node.go - host platform and task identity

Resolves executable names and Linux file ownership for unprivileged tasks.

===========================================================================
*/
package main

import (
	"fmt"
	"os/user"
	"runtime"
	"strconv"
)

/*
================
resolveTaskOwner

Nomad renders templates as its own service user unless given numeric ownership.
Resolve the task account on the deployment host, where the releases are staged.
Windows retains the service-account ACL behavior and has no Unix ownership.
================
*/
func resolveTaskOwner(name string) (int, int, error) {
	if name == "" || runtime.GOOS == "windows" {
		return -1, -1, nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return -1, -1, fmt.Errorf("resolve task user %q: %w", name, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return -1, -1, fmt.Errorf("task user %q UID: %w", name, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return -1, -1, fmt.Errorf("task user %q GID: %w", name, err)
	}
	return uid, gid, nil
}

/*
==================
nodeVariables

Adds the node-specific job variables. The deployer runs on the node it
deploys to, so the node's kernel is its own. Resource sizes and the task
account are only set when the operator chose them; otherwise the job's
defaults apply.
==================
*/
func (deployment *deployment) nodeVariables(cpu, memoryMB int, variables map[string]any) map[string]any {
	variables["node_kernel"] = runtime.GOOS
	if deployment.TaskUser != "" {
		variables["task_user"] = deployment.TaskUser
		variables["task_uid"] = deployment.TaskUID
		variables["task_gid"] = deployment.TaskGID
	}
	if cpu > 0 {
		variables["cpu"] = cpu
	}
	if memoryMB > 0 {
		variables["memory_mb"] = memoryMB
	}
	return variables
}

/*
==================
binaryName

The platform file name of a service binary: agent.exe on Windows, agent
elsewhere.
==================
*/
func binaryName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
