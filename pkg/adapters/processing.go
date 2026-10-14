// Package adapters connects local implementations to controlled module assembly.
// Factories are internal resolved-plan adapters, not deployment-schema loaders.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parser"
)

var ErrModule = errors.New("processing module unavailable or invalid")

type parserSettings struct {
	MaxBytes int64 `json:"max_bytes"`
}

func parseSettings(raw json.RawMessage) (parserSettings, error) {
	var s parserSettings
	if len(raw) > 256 {
		return s, ErrModule
	}
	if _, err := canonical.JSON(raw); err != nil {
		return s, ErrModule
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || s.MaxBytes < 1 || s.MaxBytes > 8<<20 {
		return s, ErrModule
	}
	if d.Decode(new(any)) != io.EOF {
		return s, ErrModule
	}
	return s, nil
}

func MarkdownFactory() kernel.Factory {
	return kernel.Factory{
		Manifest: kernel.Manifest{ID: "platform.markdown", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Parsing}, CapabilityVersions: map[contracts.Capability]string{contracts.Parsing: "1.0"}, CapabilityFeatures: map[contracts.Capability][]string{contracts.Parsing: {"format.markdown", "text.preserve", "source.characters"}}, DependencySlots: []kernel.Dependency{{Slot: "blob_store", Capability: contracts.BlobStorage, ContractVersion: "1.0", Features: []string{"stream.read"}}}},
		Validate: func(raw json.RawMessage) error { _, err := parseSettings(raw); return err },
		Create: func(ctx context.Context, raw json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
			if ctx == nil || lookup == nil {
				return nil, ErrModule
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s, err := parseSettings(raw)
			if err != nil {
				return nil, err
			}
			value, err := lookup("blob_store")
			if err != nil {
				return nil, ErrModule
			}
			store, ok := value.(contracts.BlobStore)
			if !ok {
				return nil, ErrModule
			}
			service := &managedParser{parser: parser.MarkdownParser{Store: store, MaxBytes: s.MaxBytes}}
			return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Parsing: service}, Health: service.health, Close: func(context.Context) error { service.closed.Store(true); return nil }}, nil
		},
	}
}

func SplitterFactory() kernel.Factory {
	validate := func(raw json.RawMessage) error {
		encoded, err := canonical.JSON(raw)
		if err != nil || string(encoded) != "{}" {
			return ErrModule
		}
		return nil
	}
	return kernel.Factory{Manifest: kernel.Manifest{ID: "platform.character-splitter", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Splitting}, CapabilityVersions: map[contracts.Capability]string{contracts.Splitting: "1.0"}, CapabilityFeatures: map[contracts.Capability][]string{contracts.Splitting: {"strategy.characters", "offset.unicode", "id.deterministic"}}}, Validate: validate,
		Create: func(ctx context.Context, raw json.RawMessage, _ kernel.Lookup) (*kernel.Instance, error) {
			if ctx == nil {
				return nil, ErrModule
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := validate(raw); err != nil {
				return nil, err
			}
			service := &managedSplitter{}
			return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Splitting: service}, Health: service.health, Close: func(context.Context) error { service.closed.Store(true); return nil }}, nil
		},
	}
}

// Dependencies are owned by the host; closing a parser never closes a shared
// blob store. Host drains in-flight calls before closing these services.
type managedParser struct {
	parser parser.MarkdownParser
	closed atomic.Bool
}

func (p *managedParser) health(ctx context.Context) error {
	if ctx == nil || p.closed.Load() {
		return ErrModule
	}
	return ctx.Err()
}
func (p *managedParser) Parse(ctx context.Context, r contracts.ParseRequest) (contracts.ParseResult, error) {
	if err := p.health(ctx); err != nil {
		return contracts.ParseResult{}, err
	}
	return p.parser.Parse(ctx, r)
}

type managedSplitter struct{ closed atomic.Bool }

func (s *managedSplitter) health(ctx context.Context) error {
	if ctx == nil || s.closed.Load() {
		return ErrModule
	}
	return ctx.Err()
}
func (s *managedSplitter) Split(ctx context.Context, r contracts.SplitRequest) ([]contracts.Chunk, error) {
	if err := s.health(ctx); err != nil {
		return nil, err
	}
	return (chunk.SimpleSplitter{}).Split(ctx, r)
}
