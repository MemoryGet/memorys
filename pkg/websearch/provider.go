package websearch

import (
	"context"
	"errors"
	"os"
	"strings"
)

var ErrUnsupportedProvider = errors.New("unsupported websearch provider")

// Provider is the application-facing web search port. Provider-specific
// request/response details stay behind this interface so another vendor can be
// selected without changing callers.
type Provider interface {
	Name() string
	Search(context.Context, SearchRequest) (SearchResponse, error)
	Fetch(context.Context, FetchRequest) (FetchResponse, error)
}

type ProviderFactory func(Config) (Provider, error)

// Config selects a provider from deployment-owned configuration. API keys are
// referenced by environment variable name and are never serialized here.
type Config struct {
	Provider   string `json:"provider"`
	APIKeyEnv  string `json:"api_key_env,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	MaxRetries int    `json:"max_retries,omitempty"`
}

type Registry struct {
	factories map[string]ProviderFactory
}

func NewRegistry(factories map[string]ProviderFactory) (*Registry, error) {
	if len(factories) == 0 {
		return nil, ErrInvalidConfig
	}
	owned := make(map[string]ProviderFactory, len(factories))
	for name, factory := range factories {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || factory == nil || owned[name] != nil {
			return nil, ErrInvalidConfig
		}
		owned[name] = factory
	}
	return &Registry{factories: owned}, nil
}

func (r *Registry) Build(cfg Config) (Provider, error) {
	if r == nil {
		return nil, ErrInvalidConfig
	}
	name := strings.ToLower(strings.TrimSpace(cfg.Provider))
	factory := r.factories[name]
	if factory == nil {
		return nil, ErrUnsupportedProvider
	}
	return factory(cfg)
}

// DefaultRegistry contains only the built-in DeepSeek adapter. Additional
// providers can be registered by the host without editing this package.
func DefaultRegistry() *Registry {
	registry, _ := NewRegistry(map[string]ProviderFactory{
		"deepseek": func(cfg Config) (Provider, error) {
			envName := strings.TrimSpace(cfg.APIKeyEnv)
			if envName == "" {
				envName = "DEEPSEEK_API_KEY"
			}
			key := os.Getenv(envName)
			options := make([]Option, 0, 2)
			if cfg.BaseURL != "" {
				options = append(options, WithBaseURL(cfg.BaseURL))
			}
			if cfg.MaxRetries != 0 {
				options = append(options, WithMaxRetries(cfg.MaxRetries))
			}
			return NewClient(key, options...)
		},
	})
	return registry
}

func NewProvider(cfg Config) (Provider, error) {
	return DefaultRegistry().Build(cfg)
}

func (c *Client) Name() string { return "deepseek" }
