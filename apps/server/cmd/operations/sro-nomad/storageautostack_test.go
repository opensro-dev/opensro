/*
===========================================================================

storageautostack_test.go - SRO_STORAGE_AUTO_STACK reaches the GameWorld job

The owner-approved deployment default is on; the deployer's own value,
including "off", passes through unchanged.

===========================================================================
*/
package main

import "testing"

/*
================
TestStorageAutoStackJobVariable
================
*/
func TestStorageAutoStackJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "on", "off": "off", "on": "on"} {
		t.Setenv("SRO_STORAGE_AUTO_STACK", env)
		deployment := &deployment{StorageAutoStack: storageAutoStackSetting()}
		if got := deployment.gameVariables(shardDeployment{})["storage_auto_stack"]; got != want {
			t.Fatalf("SRO_STORAGE_AUTO_STACK=%q: job variable %v, want %s", env, got, want)
		}
	}
}
