package main

import "testing"

func TestFixturePortRejectsHostAndConnectionOptionInjection(t *testing.T) {
	for _, value := range []string{"0", "65536", "05432", "5432/postgres?host=external.example", "5432?sslmode=require"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AKOS_TEST_PG_PORT", value)
			if db, err := openFixtureDB(); err == nil {
				db.Close()
				t.Fatal("invalid fixture connection admitted")
			}
		})
	}
	for _, value := range []string{"", "55432"} {
		t.Setenv("AKOS_TEST_PG_PORT", value)
		db, err := openFixtureDB()
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestGovernanceDiagnosticModeIsExplicit(t *testing.T) {
	for _, value := range []string{"review", "true", "review-renew ", "review-renew;extra"} {
		t.Setenv("AKOS_TEST_GOVERNANCE_REPRO", value)
		if _, e := governanceReproMode(); e == nil {
			t.Fatal("unknown diagnostic mode accepted")
		}
	}
	for _, value := range []string{"", "review-renew"} {
		t.Setenv("AKOS_TEST_GOVERNANCE_REPRO", value)
		mode, e := governanceReproMode()
		if e != nil || mode != (value != "") {
			t.Fatal("diagnostic selection differs", e)
		}
	}
}
