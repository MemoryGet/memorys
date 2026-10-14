package fusion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

// NewRRFModule captures separately verified profiles and returns the required
// settings binding. This is controlled assembly, not profile authentication.
func NewRRFModule(p contracts.PipelineProfile, q contracts.QueryProfile) (kernel.Factory, json.RawMessage, error) {
	f, err := NewRRFForProfile(p, q)
	if err != nil {
		return kernel.Factory{}, nil, err
	}
	identity, err := json.Marshal(f.Identity())
	if err != nil {
		return kernel.Factory{}, nil, ErrInvalid
	}
	hash := sha256.Sum256(identity)
	binding := hex.EncodeToString(hash[:])
	settings, _ := json.Marshal(map[string]string{"profile_identity": binding})
	validate := func(raw json.RawMessage) error {
		if len(raw) > 256 {
			return ErrInvalid
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		if token, err := d.Token(); err != nil || token != json.Delim('{') {
			return ErrInvalid
		}
		if token, err := d.Token(); err != nil || token != "profile_identity" {
			return ErrInvalid
		}
		if token, err := d.Token(); err != nil || token != binding {
			return ErrInvalid
		}
		if token, err := d.Token(); err != nil || token != json.Delim('}') {
			return ErrInvalid
		}
		if _, err := d.Token(); err != io.EOF {
			return ErrInvalid
		}
		return nil
	}
	factory := kernel.Factory{
		Manifest: kernel.Manifest{ID: "platform.rrf", Version: "1.0.0", ContractVersion: "1.0.0", Capabilities: []contracts.Capability{contracts.Fusion}},
		Validate: validate,
		Create: func(ctx context.Context, raw json.RawMessage, _ kernel.Lookup) (*kernel.Instance, error) {
			if ctx == nil {
				return nil, ErrInvalid
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := validate(raw); err != nil {
				return nil, err
			}
			service := &managedRRF{f: f}
			return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Fusion: service}, Health: service.health,
				Close: func(context.Context) error { service.closed.Store(true); return nil }}, nil
		},
	}
	return factory, settings, nil
}

type managedRRF struct {
	f      *ProfileRRF
	closed atomic.Bool
}

func (f *managedRRF) health(ctx context.Context) error {
	if ctx == nil || f.closed.Load() {
		return ErrInvalid
	}
	return ctx.Err()
}

// The host drains calls before Close. Closing rejects later calls through
// previously resolved references; already running calls are not interrupted.
func (f *managedRRF) Fuse(ctx context.Context, r contracts.FuseRequest) ([]contracts.Candidate, error) {
	if err := f.health(ctx); err != nil {
		return nil, err
	}
	return f.f.Fuse(ctx, r)
}
