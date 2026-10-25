package websearch

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Search(context.Context, SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, nil
}
func (f fakeProvider) Fetch(context.Context, FetchRequest) (FetchResponse, error) {
	return FetchResponse{}, nil
}

func TestRegistrySelectsConfiguredProvider(t *testing.T) {
	registry, err := NewRegistry(map[string]ProviderFactory{
		"custom": func(Config) (Provider, error) { return fakeProvider{name: "custom"}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := registry.Build(Config{Provider: "CUSTOM"})
	if err != nil || provider.Name() != "custom" {
		t.Fatalf("provider=%v err=%v", provider, err)
	}
	if _, err := registry.Build(Config{Provider: "missing"}); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryRejectsDuplicateNormalizedNames(t *testing.T) {
	_, err := NewRegistry(map[string]ProviderFactory{
		"deepseek": func(Config) (Provider, error) { return fakeProvider{name: "a"}, nil },
		"DeepSeek": func(Config) (Provider, error) { return fakeProvider{name: "b"}, nil },
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err=%v", err)
	}
}

func TestDefaultRegistryRequiresConfiguredKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := NewProvider(Config{Provider: "deepseek"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err=%v", err)
	}
}
