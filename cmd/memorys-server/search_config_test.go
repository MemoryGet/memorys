package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func searchSettingsLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, exists := values[key]
		return value, exists
	}
}

func validSearchSettings(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{
		"MEMORYS_SEARCH_ENABLED":     "true",
		"MEMORYS_DATABASE_URL":       "postgres://user:database-secret@127.0.0.1/memorys",
		"MEMORYS_OAUTH_ISSUER":       "https://memorys.local",
		"MEMORYS_OAUTH_AUDIENCE":     "memorys-search",
		"MEMORYS_OAUTH_SUBJECT_TYPE": "service",
		"MEMORYS_OAUTH_JWKS_FILE":    filepath.Join(dir, "jwks.json"),
		"MEMORYS_PROFILE_FILE":       filepath.Join(dir, "profile.json"),
	}
}

func TestLoadSearchSettingsRequiresExplicitEnable(t *testing.T) {
	for _, enabled := range []string{"unset", "false"} {
		t.Run(enabled, func(t *testing.T) {
			values := validSearchSettings(t)
			delete(values, "MEMORYS_SEARCH_ENABLED")
			if enabled == "false" {
				values["MEMORYS_SEARCH_ENABLED"] = enabled
			}
			// Disabled search must not inspect unrelated or malformed settings.
			values["MEMORYS_OAUTH_ISSUER"] = "not-a-url"
			values["MEMORYS_OAUTH_TOKEN_MAX_LIFETIME"] = "invalid"
			settings, err := loadSearchSettings(searchSettingsLookup(values))
			if err != nil || settings != (searchSettings{}) {
				t.Fatalf("disabled search = %#v, %v; want zero settings and no error", settings, err)
			}
		})
	}
}

func TestLoadSearchSettingsEnabled(t *testing.T) {
	values := validSearchSettings(t)
	settings, err := loadSearchSettings(searchSettingsLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	want := searchSettings{
		Enabled:          true,
		Issuer:           values["MEMORYS_OAUTH_ISSUER"],
		Audience:         values["MEMORYS_OAUTH_AUDIENCE"],
		SubjectType:      values["MEMORYS_OAUTH_SUBJECT_TYPE"],
		JWKSFile:         values["MEMORYS_OAUTH_JWKS_FILE"],
		ProfileFile:      values["MEMORYS_PROFILE_FILE"],
		MaxTokenLifetime: 15 * time.Minute,
	}
	if settings != want {
		t.Fatalf("settings = %#v; want %#v", settings, want)
	}
}

func TestLoadSearchSettingsRejectsInvalidSubjectType(t *testing.T) {
	values := validSearchSettings(t)
	values["MEMORYS_OAUTH_SUBJECT_TYPE"] = "administrator"
	if _, err := loadSearchSettings(searchSettingsLookup(values)); err == nil {
		t.Fatal("unsupported subject type admitted")
	}
}

func TestLoadSearchSettingsRejectsInvalidEnable(t *testing.T) {
	for _, value := range []string{"", "TRUE", "1", "yes", " true", "false "} {
		t.Run(value, func(t *testing.T) {
			settings, err := loadSearchSettings(searchSettingsLookup(map[string]string{"MEMORYS_SEARCH_ENABLED": value}))
			if err == nil || settings != (searchSettings{}) {
				t.Fatal("invalid explicit enable must fail with zero settings")
			}
		})
	}
}

func TestLoadSearchSettingsRequiresFields(t *testing.T) {
	for _, field := range []string{"MEMORYS_DATABASE_URL", "MEMORYS_OAUTH_ISSUER", "MEMORYS_OAUTH_AUDIENCE", "MEMORYS_OAUTH_SUBJECT_TYPE", "MEMORYS_OAUTH_JWKS_FILE", "MEMORYS_PROFILE_FILE"} {
		for _, missing := range []string{"unset", "empty", "blank"} {
			t.Run(field+"/"+missing, func(t *testing.T) {
				values := validSearchSettings(t)
				delete(values, field)
				if missing == "empty" {
					values[field] = ""
				} else if missing == "blank" {
					values[field] = " \t"
				}
				settings, err := loadSearchSettings(searchSettingsLookup(values))
				if err == nil || settings != (searchSettings{}) || !strings.Contains(err.Error(), field) {
					t.Fatalf("missing field must fail with zero settings and field-specific error, got %v", err)
				}
			})
		}
	}
}

func TestLoadSearchSettingsValidatesIssuer(t *testing.T) {
	for _, issuer := range []string{
		"http://memorys.local", "memorys.local", "/issuer", "https:///issuer", "https:memorys.local",
		"https://user:issuer-secret@memorys.local", "https://memorys.local?token=issuer-secret",
		"https://memorys.local#issuer-secret", "https://memorys.local?", "https://memorys.local#",
		"https://memorys.local/%", "https://issuer-secret.invalid:bad",
	} {
		t.Run(issuer, func(t *testing.T) {
			values := validSearchSettings(t)
			values["MEMORYS_OAUTH_ISSUER"] = issuer
			settings, err := loadSearchSettings(searchSettingsLookup(values))
			if err == nil || settings != (searchSettings{}) {
				t.Fatal("invalid issuer must fail with zero settings")
			}
			if strings.Contains(err.Error(), "issuer-secret") || strings.Contains(err.Error(), "database-secret") {
				t.Fatal("configuration error contains a secret")
			}
		})
	}
	values := validSearchSettings(t)
	values["MEMORYS_OAUTH_ISSUER"] = "https://memorys.local/oauth/issuer"
	if _, err := loadSearchSettings(searchSettingsLookup(values)); err != nil {
		t.Fatalf("HTTPS issuer with a path must be accepted: %v", err)
	}
}

func TestLoadSearchSettingsRequiresAbsolutePaths(t *testing.T) {
	for _, field := range []string{"MEMORYS_OAUTH_JWKS_FILE", "MEMORYS_PROFILE_FILE"} {
		t.Run(field, func(t *testing.T) {
			values := validSearchSettings(t)
			values[field] = filepath.Join("path-secret", "config.json")
			settings, err := loadSearchSettings(searchSettingsLookup(values))
			if err == nil || settings != (searchSettings{}) || !strings.Contains(err.Error(), field) {
				t.Fatal("relative path must fail with zero settings and a field-specific error")
			}
			if strings.Contains(err.Error(), "path-secret") {
				t.Fatal("configuration error contains the path value")
			}
		})
	}
}

func TestLoadSearchSettingsTokenLifetime(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"1s", time.Second}, {"90s", 90 * time.Second}, {"1m30s", 90 * time.Second},
		{"1500ms", 0}, {"24h", 24 * time.Hour}, {"86400s", 24 * time.Hour},
		{"0s", 0}, {"-1s", 0}, {"1ns", 0}, {"24h1s", 0}, {"", 0}, {"lifetime-secret", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			values := validSearchSettings(t)
			values["MEMORYS_OAUTH_TOKEN_MAX_LIFETIME"] = tc.value
			settings, err := loadSearchSettings(searchSettingsLookup(values))
			if tc.want == 0 {
				if err == nil || settings != (searchSettings{}) {
					t.Fatal("invalid lifetime must fail with zero settings")
				}
				if strings.Contains(err.Error(), "lifetime-secret") {
					t.Fatal("configuration error contains the lifetime value")
				}
				return
			}
			if err != nil || settings.MaxTokenLifetime != tc.want {
				t.Fatalf("lifetime = %v, %v; want %v", settings.MaxTokenLifetime, err, tc.want)
			}
		})
	}
}
