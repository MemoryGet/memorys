package kernel_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestLoadConfigFileRejectsUnknownAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "modules.json")
	raw, err := json.Marshal(kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "reader", Provider: "postgres", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Retrieval: "reader"}})
	if err != nil || os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("could not write module config")
	}
	if cfg, err := kernel.LoadConfigFile(path); err != nil || cfg.Version != 1 || len(cfg.Modules) != 1 {
		t.Fatal("valid module config rejected", err)
	}
	if os.WriteFile(path, append(raw, []byte(" {}")...), 0600) != nil {
		t.Fatal("could not write trailing config")
	}
	if _, err := kernel.LoadConfigFile(path); err == nil {
		t.Fatal("trailing module config accepted")
	}
	if os.WriteFile(path, []byte(`{"version":1,"modules":[],"bindings":{},"extra":true}`), 0600) != nil {
		t.Fatal("could not write unknown config")
	}
	if _, err := kernel.LoadConfigFile(path); err == nil {
		t.Fatal("unknown module config field accepted")
	}
}
