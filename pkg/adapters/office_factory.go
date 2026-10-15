package adapters

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parser"
)

// OfficeFactory uses trusted host converter/health wiring and a scoped blob
// dependency. It is a resolved-plan candidate module, not a deployment loader.
func OfficeFactory(converter parser.OfficeConverter, health func(context.Context) error) kernel.Factory {
	return projectionFactory("platform.markitdown-office", "0.1.3", []string{"docx", "xlsx", "pptx", "pdf"}, "projection.markdown", converter, health)
}

func PyPDFFactory(converter parser.OfficeConverter, health func(context.Context) error) kernel.Factory {
	return projectionFactory("platform.pypdf", "6.19.0", []string{"pdf"}, "projection.text", converter, health)
}

func projectionFactory(id, version string, formats []string, projection string, converter parser.OfficeConverter, health func(context.Context) error) kernel.Factory {
	features := []string{"source.coarse", projection}
	for _, format := range formats {
		features = append(features, "format."+format)
	}
	validate := func(raw json.RawMessage) error {
		if missingPort(converter) || health == nil {
			return ErrModule
		}
		_, err := parseSettings(raw)
		return err
	}
	return kernel.Factory{Manifest: kernel.Manifest{ID: id, Version: version, ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Parsing}, CapabilityVersions: map[contracts.Capability]string{contracts.Parsing: "1.0"}, CapabilityFeatures: map[contracts.Capability][]string{contracts.Parsing: features}, DependencySlots: []kernel.Dependency{{Slot: "blob_store", Capability: contracts.BlobStorage, ContractVersion: "1.0", Features: []string{"stream.read"}}}}, Validate: validate, Create: func(ctx context.Context, raw json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		if ctx == nil || lookup == nil || ctx.Err() != nil || validate(raw) != nil {
			return nil, ErrModule
		}
		settings, err := parseSettings(raw)
		if err != nil {
			return nil, err
		}
		value, err := lookup("blob_store")
		if err != nil {
			return nil, err
		}
		store, ok := value.(contracts.BlobStore)
		if !ok || missingPort(store) {
			return nil, ErrModule
		}
		if err := health(ctx); err != nil {
			return nil, err
		}
		service := &managedOffice{parser: parser.OfficeParser{Store: store, Converter: converter, MaxBytes: settings.MaxBytes}, formats: append([]string(nil), formats...)}
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Parsing: service}, Health: func(c context.Context) error {
			if err := service.check(c); err != nil {
				return err
			}
			return health(c)
		}, Close: func(context.Context) error { service.closed.Store(true); return nil }}, nil
	}}
}

type managedOffice struct {
	parser  parser.OfficeParser
	closed  atomic.Bool
	formats []string
}

func (p *managedOffice) check(ctx context.Context) error {
	if ctx == nil || p.closed.Load() {
		return ErrModule
	}
	return ctx.Err()
}
func (p *managedOffice) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	if err := p.check(ctx); err != nil {
		return contracts.ParseResult{}, err
	}
	allowed := false
	for _, format := range p.formats {
		allowed = allowed || req.Format == format
	}
	if !allowed {
		return contracts.ParseResult{}, ErrModule
	}
	return p.parser.Parse(ctx, req)
}
