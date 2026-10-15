package architecture_test

import (
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/MemoryGet/memorys/"

// Explicit package inventory: new packages need a reviewed dependency policy.
// Tests/fakes can compose implementations; only non-test Go source is gated.
var localDependencies = map[string][]string{
	"pkg/preparation":                  {"pkg/contracts", "pkg/document", "pkg/canonical", "pkg/indexsnapshot"},
	"cmd/architecture-freeze-draft":    {"pkg/canonical", "pkg/contracts", "pkg/document", "pkg/modulewire"},
	"cmd/architecture-office-parser":   {"pkg/contracts", "pkg/adapters", "pkg/kernel", "pkg/parserprocess", "pkg/storage", "pkg/document"},
	"pkg/parserprocess":                {"pkg/parser"},
	"cmd/architecture-pg-repositories": {"pkg/contracts", "pkg/postgres", "pkg/withdrawal", "pkg/cleanup", "pkg/adapters", "pkg/index", "pkg/kernel", "pkg/document", "pkg/ingestion", "pkg/parser", "pkg/storage", "pkg/preparation", "pkg/chunk", "pkg/modulewire", "pkg/queryread"},
	"pkg/cleanup":                      {"pkg/contracts", "pkg/canonical"},
	"pkg/withdrawal":                   {"pkg/contracts", "pkg/canonical"},
	"pkg/postgres":                     {"pkg/contracts", "pkg/indexsnapshot", "pkg/document", "pkg/preparation", "pkg/queryread"},
	"pkg/indexsnapshot":                {"pkg/contracts", "pkg/canonical"},
	"pkg/adapters":                     {"pkg/canonical", "pkg/chunk", "pkg/contracts", "pkg/kernel", "pkg/parser", "pkg/cleanup", "pkg/index"},
	"cmd/memorys-server":               {"pkg/api"},
	"pkg/api":                          {"pkg/contracts", "pkg/ingestion"},
	"pkg/chunk":                        {"pkg/contracts"},
	"pkg/document":                     {"pkg/contracts", "pkg/canonical"},
	"pkg/index":                        {"pkg/contracts", "pkg/indexsnapshot"},
	"pkg/ingestion":                    {"pkg/contracts", "pkg/document"},
	"pkg/parser":                       {"pkg/contracts"},
	"pkg/retrieval":                    {"pkg/contracts", "pkg/index"},
	"pkg/storage":                      {"pkg/contracts"},

	"cmd/architecture-graph-cleanup-digest": {"pkg/modulewire"},
	"cmd/architecture-memory-record":        {"pkg/modulewire"},
	"pkg/contracts":                         {},
	"pkg/canonical":                         {},
	"pkg/kernel":                            {"pkg/contracts"},
	"pkg/fusion":                            {"pkg/contracts", "pkg/kernel"}, // Local implementation's factory.
	"pkg/retrievalplan":                     {"pkg/contracts"},
	"pkg/reflection":                        {"pkg/contracts"},
	"pkg/queryread":                         {"pkg/contracts"},
	"pkg/contentverify":                     {"pkg/contracts", "pkg/canonical"},
	"pkg/publicationprojection":             {"pkg/contracts", "pkg/canonical"},
	"pkg/modulewire":                        {"pkg/contracts", "pkg/canonical"},
	"pkg/moduletransport":                   {"pkg/contracts", "pkg/modulewire"},
	"pkg/accessgrant":                       {"pkg/canonical"},
	"cmd/architecture-parser-wire":          {"pkg/contracts", "pkg/canonical", "pkg/modulewire"},
	"cmd/architecture-store-binding":        {"pkg/modulewire"},
	"cmd/architecture-store-probe":          {"pkg/modulewire"},
	"cmd/architecture-trust-record":         {"pkg/modulewire"},
	"cmd/architecture-projection-digest":    {"pkg/publicationprojection"},
}

var purePackages = map[string]bool{
	"pkg/preparation":   true,
	"pkg/indexsnapshot": true,
	"pkg/cleanup":       true,
	"pkg/withdrawal":    true,
	"pkg/adapters":      true,
	"pkg/chunk":         true, "pkg/document": true, "pkg/ingestion": true, "pkg/index": true, "pkg/parser": true, "pkg/retrieval": true,
	"pkg/contracts": true, "pkg/canonical": true,
	"pkg/retrievalplan": true, "pkg/reflection": true, "pkg/queryread": true,
	"pkg/publicationprojection": true,
	"pkg/contentverify":         true,
}

func allowedImport(owner, dependency string) bool {
	allowed, registered := localDependencies[owner]
	if !registered {
		return false
	}
	if strings.HasPrefix(dependency, module) {
		for _, local := range allowed {
			if dependency == module+local {
				return true
			}
		}
		return false
	}
	if owner == "cmd/architecture-pg-repositories" && dependency == "github.com/jackc/pgx/v5/stdlib" {
		return true
	}
	if owner == "pkg/accessgrant" && dependency == "github.com/go-jose/go-jose/v4" {
		return true // Isolated cryptographic implementation; never exposed in contracts.
	}
	if dependency == "C" || dependency == "plugin" || dependency == "unsafe" {
		return false
	}
	if purePackages[owner] && (dependency == "net" || strings.HasPrefix(dependency, "net/") || strings.HasPrefix(dependency, "database/") || dependency == "os/exec") {
		return false
	}
	// Resolve against the actual Go standard library, not a heuristic that treats
	// every dot-free import as stdlib (which would allow arbitrary local paths).
	p, err := build.Default.Import(dependency, "", build.FindOnly)
	return err == nil && p.Goroot
}

func sourceViolations(owner, filename, source string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var bad []string
	if _, ok := localDependencies[owner]; !ok {
		bad = append(bad, "unregistered package: "+owner)
	}
	for _, im := range f.Imports {
		path, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			return nil, err
		}
		if !allowedImport(owner, path) {
			bad = append(bad, "forbidden import: "+path)
		}
	}
	return bad, nil
}

func TestProductionDependencyBoundaries(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	files := 0
	for _, tree := range []string{"pkg", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			owner, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			owner = filepath.ToSlash(owner)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			files++
			if _, ok := localDependencies[owner]; !ok {
				t.Errorf("%s: unregistered package %s", path, owner)
			}
			for _, im := range f.Imports {
				dependency, err := strconv.Unquote(im.Path.Value)
				if err != nil {
					return err
				}
				if !allowedImport(owner, dependency) {
					t.Errorf("%s: %s cannot import %s", path, owner, dependency)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files == 0 {
		t.Fatal("no production files scanned")
	}
	t.Logf("checked %d non-test Go files including all build tags", files)
}

func TestDependencyGateRejectsBoundaryViolations(t *testing.T) {
	for _, tc := range []struct{ name, owner, source string }{
		{"vendor-type", "pkg/contracts", `package contracts; import sdk "github.com/Tencent/WeKnora/internal/types"`},
		{"sql-in-contract", "pkg/contracts", `package contracts; import "database/sql"`},
		{"http-in-application", "pkg/reflection", `package reflection; import "net/http"`},
		{"concrete-module", "pkg/retrievalplan", `package retrievalplan; import "github.com/MemoryGet/memorys/pkg/fusion"`},
		{"new-unreviewed-package", "pkg/newmodule", `package newmodule`},
		{"dot-free-not-stdlib", "pkg/contracts", `package contracts; import "unreviewed/sdk"`},
		{"blank-import", "pkg/contracts", `package contracts; import _ "github.com/MemoryGet/memorys/pkg/modulewire"`},
		{"crypto-leak", "pkg/contracts", `package contracts; import "github.com/go-jose/go-jose/v4"`},
		{"dynamic-plugin", "pkg/kernel", `package kernel; import "plugin"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad, err := sourceViolations(tc.owner, "fixture.go", tc.source)
			if err != nil || len(bad) == 0 {
				t.Fatalf("gate failed: %v, %v", bad, err)
			}
		})
	}
}

func TestDependencyGateAllowsPortsAndIsolatedSDK(t *testing.T) {
	for _, tc := range []struct{ owner, source string }{
		{"pkg/contracts", `package contracts; import "context"; import "math/big"`},
		{"pkg/reflection", `package reflection; import "github.com/MemoryGet/memorys/pkg/contracts"`},
		{"pkg/accessgrant", `package accessgrant; import "github.com/go-jose/go-jose/v4"`},
		{"pkg/moduletransport", `package moduletransport; import "net/http"; import "github.com/MemoryGet/memorys/pkg/modulewire"`},
	} {
		bad, err := sourceViolations(tc.owner, "fixture.go", tc.source)
		if err != nil || len(bad) != 0 {
			t.Fatalf("valid boundary rejected: %s: %v, %v", tc.owner, bad, err)
		}
	}
}
