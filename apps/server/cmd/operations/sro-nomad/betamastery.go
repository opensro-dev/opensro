/*
===========================================================================
betamastery.go - persistent operator switch for beta mastery training

The state directory survives component releases. Keeping the policy there
prevents a later deployment from silently restoring the beta default.
===========================================================================
*/
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const betaMasteryPolicyFile = "beta-mastery.txt"

/*
================
configuredBetaMastery

Native limits unless the operator puts on in the state-directory file
(owner, 2026-11-13: restore the native Chinese 300 / European
min(2 x level, 240) totals). The file survives every later deployment.
================
*/
func configuredBetaMastery(stateDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, betaMasteryPolicyFile))
	if os.IsNotExist(err) {
		return "off", nil
	}
	if err != nil {
		return "", fmt.Errorf("beta mastery policy: %w", err)
	}
	switch value := strings.ToLower(strings.TrimSpace(string(data))); value {
	case "on", "off":
		return value, nil
	default:
		return "", fmt.Errorf("%s must contain on or off", betaMasteryPolicyFile)
	}
}
