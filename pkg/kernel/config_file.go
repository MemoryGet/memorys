package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

var ErrConfigFile = errors.New("module configuration file rejected")

// LoadConfigFile reads one deployment-owned module configuration snapshot. It
// only decodes the external shape; Build/BuildBound performs factory,
// dependency, feature and health validation before creating modules.
func LoadConfigFile(path string) (Config, error) {
	var out Config
	if path == "" {
		return out, ErrConfigFile
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return out, ErrConfigFile
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil {
		return Config{}, ErrConfigFile
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Config{}, ErrConfigFile
	}
	return out, nil
}
