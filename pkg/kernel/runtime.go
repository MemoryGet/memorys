package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

const operationTimeout = 5 * time.Second

type Runtime struct {
	mu        sync.RWMutex
	closed    bool
	closeDone chan struct{}
	closeErr  error
	instances map[string]*Instance
	order     []string
	snapshot  Snapshot
}

func cloneSnapshot(input Snapshot) Snapshot {
	result := input
	result.Bindings = copyMap(input.Bindings)
	result.Modules = append([]ModuleSnapshot(nil), input.Modules...)
	for i := range result.Modules {
		module := &result.Modules[i]
		module.Capabilities = append([]contracts.Capability(nil), module.Capabilities...)
		module.CapabilityVersions = copyMap(module.CapabilityVersions)
		module.CapabilityFeatures = copyMap(module.CapabilityFeatures)
		for cap, features := range module.CapabilityFeatures {
			module.CapabilityFeatures[cap] = append([]string(nil), features...)
		}
	}
	return result
}

func makeSnapshot(config Config, modules map[string]*plannedModule, order []string) Snapshot {
	result := Snapshot{Bindings: copyMap(config.Bindings)}
	resolved := cloneConfig(config)
	for i := range resolved.Modules {
		resolved.Modules[i] = modules[resolved.Modules[i].ID].config
	}
	sort.Slice(resolved.Modules, func(i, j int) bool { return resolved.Modules[i].ID < resolved.Modules[j].ID })
	for _, id := range order {
		manifest := modules[id].factory.Manifest
		result.Modules = append(result.Modules, ModuleSnapshot{ID: id, Provider: manifest.ID, ImplementationVersion: manifest.Version,
			ContractVersion: manifest.ContractVersion, Capabilities: manifest.Capabilities, CapabilityVersions: manifest.CapabilityVersions, CapabilityFeatures: manifest.CapabilityFeatures})
	}
	data, _ := json.Marshal(struct {
		Plan    Config
		Modules []ModuleSnapshot
	}{resolved, result.Modules})
	digest := sha256.Sum256(data)
	result.ConfigVersion = hex.EncodeToString(digest[:])
	return cloneSnapshot(result)
}

func (runtime *Runtime) Snapshot() Snapshot {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return cloneSnapshot(runtime.snapshot)
}

func (runtime *Runtime) Get(cap contracts.Capability) (any, error) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.closed {
		return nil, errors.New("module runtime is closed")
	}
	id := runtime.snapshot.Bindings[cap]
	instance := runtime.instances[id]
	if instance == nil || instance.Services[cap] == nil {
		return nil, errors.New("capability is not bound")
	}
	return instance.Services[cap], nil
}

func Resolve[T any](runtime *Runtime, cap contracts.Capability) (T, error) {
	var zero T
	if runtime == nil {
		return zero, errors.New("module runtime is missing")
	}
	service, err := runtime.Get(cap)
	if err != nil {
		return zero, err
	}
	result, ok := service.(T)
	if !ok || nilService(result) {
		return zero, errors.New("requested port type does not match")
	}
	return result, nil
}

// Close requires the application to drain in-flight calls first. It invalidates
// future lookup; it cannot revoke service references already handed to callers.
func (runtime *Runtime) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("close context is missing")
	}
	runtime.mu.Lock()
	if runtime.closed {
		done := runtime.closeDone
		runtime.mu.Unlock()
		select {
		case <-done:
			runtime.mu.RLock()
			defer runtime.mu.RUnlock()
			return runtime.closeErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	runtime.closed = true
	instances, order := runtime.instances, append([]string(nil), runtime.order...)
	runtime.mu.Unlock()
	var failure error
	for i := len(order) - 1; i >= 0; i-- {
		instance := instances[order[i]]
		if instance != nil && instance.Close != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
			if callBounded(cleanupCtx, instance.Close) != nil {
				failure = errors.New("one or more modules failed to close")
			}
			cancel()
		}
	}
	runtime.mu.Lock()
	runtime.closeErr = failure
	runtime.instances = nil
	close(runtime.closeDone)
	runtime.mu.Unlock()
	return failure
}

func callBounded(ctx context.Context, call func(context.Context) error) error {
	result := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if recover() != nil {
				err = errors.New("module callback failed")
			}
			result <- err
		}()
		err = call(ctx)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type createResult struct {
	instance *Instance
	err      error
}

func callCreate(ctx context.Context, factory Factory, settings json.RawMessage, lookup Lookup) (*Instance, error) {
	result := make(chan createResult)
	go func() {
		value := createResult{}
		defer func() {
			if recover() != nil {
				value.err = errors.New("module creation failed")
			}
			select {
			case result <- value:
			case <-ctx.Done():
				if value.instance != nil && value.instance.Close != nil {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), operationTimeout)
					_ = callBounded(cleanupCtx, value.instance.Close)
					cancel()
				}
			}
		}()
		value.instance, value.err = factory.Create(ctx, append(json.RawMessage(nil), settings...), lookup)
	}()
	select {
	case value := <-result:
		return value.instance, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func validateInstance(instance *Instance, manifest Manifest) error {
	if instance == nil || instance.Health == nil || instance.Close == nil {
		return errors.New("module instance lifecycle is incomplete")
	}
	if len(instance.Services) != len(manifest.Capabilities) {
		return errors.New("module services do not match its declaration")
	}
	for _, cap := range manifest.Capabilities {
		service := instance.Services[cap]
		if nilService(service) || !portChecks[cap](service) {
			return errors.New("module service does not implement the declared port")
		}
	}
	return nil
}

func dependencyLookup(module *plannedModule, instances map[string]*Instance) Lookup {
	services := make(map[string]any)
	for slot, dep := range module.dependencies {
		services[slot] = instances[dep.instance].Services[dep.capability]
	}
	return func(slot string) (any, error) {
		service, exists := services[slot]
		if !exists {
			return nil, errors.New("dependency slot is not declared")
		}
		return service, nil
	}
}

func createModule(ctx context.Context, module *plannedModule, runtime *Runtime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	createCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	instance, err := callCreate(createCtx, module.factory, module.config.Settings, dependencyLookup(module, runtime.instances))
	cancel()
	if instance != nil {
		owned := *instance
		owned.Services = copyMap(instance.Services)
		instance = &owned
		runtime.instances[module.config.ID] = instance
		runtime.order = append(runtime.order, module.config.ID)
	}
	if err != nil {
		return errors.New("module creation failed")
	}
	if err := validateInstance(instance, module.factory.Manifest); err != nil {
		return err
	}
	healthCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	err = callBounded(healthCtx, instance.Health)
	cancel()
	if err != nil {
		return errors.New("module startup health check failed")
	}
	instance.Services = copyMap(instance.Services)
	return nil
}

// Build consumes a resolved internal plan and caller-specified requirements.
// It does not infer full product readiness from a parser-only plan.
func Build(ctx context.Context, config Config, factories []Factory, requirements ...Requirement) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("module build context is missing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config = cloneConfig(config)
	modules, order, err := preflight(config, factories, requirements)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{instances: make(map[string]*Instance), closeDone: make(chan struct{}), snapshot: makeSnapshot(config, modules, order)}
	for _, id := range order {
		if err := createModule(ctx, modules[id], runtime); err != nil {
			_ = runtime.Close(context.Background())
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	return runtime, nil
}
