package main

import (
	"errors"
	"os"
)

// Diagnostic-only checkpoint; this sentinel must never yield a full-probe pass.
var errGovernanceReproComplete = errors.New("governance diagnostic checkpoint complete")

func governanceReproMode() (bool, error) {
	switch os.Getenv("AKOS_TEST_GOVERNANCE_REPRO") {
	case "":
		return false, nil
	case "review-renew":
		return true, nil
	default:
		return false, errors.New("invalid governance diagnostic mode")
	}
}
