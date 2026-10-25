package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type searchSettings struct {
	Enabled          bool
	Issuer           string
	Audience         string
	SubjectType      string
	JWKSFile         string
	ProfileFile      string
	MaxTokenLifetime time.Duration
}

// loadSearchSettings keeps search opt-in even when OAuth settings are present.
// Errors identify configuration fields without including their values.
func loadSearchSettings(lookup func(string) (string, bool)) (searchSettings, error) {
	if lookup == nil {
		return searchSettings{}, errors.New("search configuration lookup required")
	}
	enabled, exists := lookup("MEMORYS_SEARCH_ENABLED")
	if !exists || enabled == "false" {
		return searchSettings{}, nil
	}
	if enabled != "true" {
		return searchSettings{}, errors.New("search configuration: MEMORYS_SEARCH_ENABLED must be true or false")
	}
	if raw, _ := lookup("MEMORYS_DATABASE_URL"); strings.TrimSpace(raw) == "" {
		return searchSettings{}, errors.New("search configuration: MEMORYS_DATABASE_URL is required")
	}

	settings := searchSettings{Enabled: true, MaxTokenLifetime: 15 * time.Minute}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"MEMORYS_OAUTH_ISSUER", &settings.Issuer},
		{"MEMORYS_OAUTH_AUDIENCE", &settings.Audience},
		{"MEMORYS_OAUTH_SUBJECT_TYPE", &settings.SubjectType},
		{"MEMORYS_OAUTH_JWKS_FILE", &settings.JWKSFile},
		{"MEMORYS_PROFILE_FILE", &settings.ProfileFile},
	} {
		value, _ := lookup(field.name)
		if strings.TrimSpace(value) == "" {
			return searchSettings{}, errors.New("search configuration: " + field.name + " is required")
		}
		*field.target = value
	}

	issuer, err := url.Parse(settings.Issuer)
	if err != nil || !issuer.IsAbs() || issuer.Scheme != "https" || issuer.Hostname() == "" || issuer.User != nil || issuer.Opaque != "" || strings.ContainsAny(settings.Issuer, "?#") {
		return searchSettings{}, errors.New("search configuration: MEMORYS_OAUTH_ISSUER must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if !filepath.IsAbs(settings.JWKSFile) {
		return searchSettings{}, errors.New("search configuration: MEMORYS_OAUTH_JWKS_FILE must be an absolute path")
	}
	if !filepath.IsAbs(settings.ProfileFile) {
		return searchSettings{}, errors.New("search configuration: MEMORYS_PROFILE_FILE must be an absolute path")
	}
	if settings.SubjectType != "user" && settings.SubjectType != "service" {
		return searchSettings{}, errors.New("search configuration: MEMORYS_OAUTH_SUBJECT_TYPE must be user or service")
	}
	if raw, exists := lookup("MEMORYS_OAUTH_TOKEN_MAX_LIFETIME"); exists {
		lifetime, err := time.ParseDuration(raw)
		if err != nil || lifetime < time.Second || lifetime > 24*time.Hour || lifetime%time.Second != 0 {
			return searchSettings{}, errors.New("search configuration: MEMORYS_OAUTH_TOKEN_MAX_LIFETIME must be whole seconds between 1s and 24h")
		}
		settings.MaxTokenLifetime = lifetime
	}
	return settings, nil
}
